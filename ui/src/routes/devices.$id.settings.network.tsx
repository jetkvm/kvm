import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { useCallback, useEffect, useRef, useState } from "react";
import { FieldValues, FormProvider, useForm } from "react-hook-form";
import { LuEthernetPort } from "react-icons/lu";
import validator from "validator";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { NetworkSettings, NetworkState, useNetworkStateStore, useRTCStore } from "@/hooks/stores";
import notifications from "@/notifications";
import { getNetworkSettings, getNetworkState } from "@/utils/jsonrpc";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField, { InputFieldWithLabel } from "@components/InputField";

import AutoHeight from "../components/AutoHeight";
import DhcpLeaseCard from "../components/DhcpLeaseCard";
import EmptyCard from "../components/EmptyCard";
import Ipv6NetworkCard from "../components/Ipv6NetworkCard";
import StaticIpv4Card from "../components/StaticIpv4Card";
import StaticIpv6Card from "../components/StaticIpv6Card";
import { useJsonRpc } from "../hooks/useJsonRpc";
import { SettingsItem } from "../components/SettingsItem";

dayjs.extend(relativeTime);

const resolveOnRtcReady = () => {
  return new Promise(resolve => {
    // Check if RTC is already connected
    const currentState = useRTCStore.getState();
    if (currentState.rpcDataChannel?.readyState === "open") {
      // Already connected, fetch data immediately
      return resolve(void 0);
    }

    // Not connected yet, subscribe to state changes
    const unsubscribe = useRTCStore.subscribe(state => {
      if (state.rpcDataChannel?.readyState === "open") {
        unsubscribe(); // Clean up subscription
        return resolve(void 0);
      }
    });
  });
};

export function LifeTimeLabel({ lifetime }: { lifetime: string }) {
  const [remaining, setRemaining] = useState<string | null>(null);

  useEffect(() => {
    setRemaining(dayjs(lifetime).fromNow());

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  if (lifetime == "") {
    return <strong>N/A</strong>;
  }

  return (
    <>
      <span className="text-sm font-medium">{remaining && <> {remaining}</>}</span>
      <span className="text-xs text-slate-700 dark:text-slate-300">
        {" "}
        ({dayjs(lifetime).format("YYYY-MM-DD HH:mm")})
      </span>
    </>
  );
}

export default function SettingsNetworkRoute() {
  const { send } = useJsonRpc();

  const networkState = useNetworkStateStore(state => state);
  const setNetworkState = useNetworkStateStore(state => state.setNetworkState);

  // Some input needs direct state management. Mostly options that open more details
  const [customDomain, setCustomDomain] = useState<string>("");

  // Confirm dialog
  const [showRenewLeaseConfirm, setShowRenewLeaseConfirm] = useState(false);
  const initialSettingsRef = useRef<NetworkSettings | null>(null);

  const [showCriticalSettingsConfirm, setShowCriticalSettingsConfirm] = useState(false);
  const [stagedSettings, setStagedSettings] = useState<NetworkSettings | null>(null);
  const [criticalChanges, setCriticalChanges] = useState<
    { label: string; from: string; to: string }[]
  >([]);

  const fetchNetworkData = useCallback(async () => {
    try {
      console.log("Fetching network data...");

      const [settings, state] = (await Promise.all([
        getNetworkSettings(),
        getNetworkState(),
      ])) as [NetworkSettings, NetworkState];

      setNetworkState(state as NetworkState);

      const settingsWithDefaults = {
        ...settings,

        domain: settings.domain || "local", // TODO: null means local domain TRUE?????
        mdns_mode: settings.mdns_mode || "disabled",
        time_sync_mode: settings.time_sync_mode || "ntp_only",
        ipv4_static: {
          address: settings.ipv4_static?.address || state.dhcp_lease?.ip || "",
          netmask: settings.ipv4_static?.netmask || state.dhcp_lease?.netmask || "",
          gateway: settings.ipv4_static?.gateway || state.dhcp_lease?.routers?.[0] || "",
          dns: settings.ipv4_static?.dns || state.dhcp_lease?.dns_servers || [],
        },
        ipv6_static: {
          prefix: settings.ipv6_static?.prefix || state.ipv6_addresses?.[0]?.prefix || "",
          gateway: settings.ipv6_static?.gateway || "",
          dns: settings.ipv6_static?.dns || [],
        },
      };

      initialSettingsRef.current = settingsWithDefaults;
      return { settings: settingsWithDefaults, state };
    } catch (err) {
      notifications.error(err instanceof Error ? err.message : "Unknown error");
      throw err;
    }
  }, []);

  const formMethods = useForm<NetworkSettings>({
    mode: "onBlur",

    defaultValues: async () => {
      console.log("Preparing form default values...");

      // Ensure data channel is ready, before fetching network data from the device
      await resolveOnRtcReady();

      const { settings } = await fetchNetworkData();
      return settings;
    },
  });

  const prepareSettings = (data: FieldValues) => {
    return {
      ...data,

      // If custom domain option is selected, use the custom domain as value
      domain: data.domain === "custom" ? customDomain : data.domain,
    } as NetworkSettings;
  };

  const { register, handleSubmit, watch, formState, reset } = formMethods;

  const onSubmit = async (settings: NetworkSettings) => {
    send("setNetworkSettings", { settings }, async (resp: any) => {
      if ("error" in resp) {
        return notifications.error(
          resp.error.data ? resp.error.data : resp.error.message,
        );
      } else {
        // If the settings are saved successfully, fetch the latest network data and reset the form
        // We do this so we get all the form state values, for stuff like is the form dirty, etc...
        const networkData = await fetchNetworkData();
        reset(networkData.settings);
        notifications.success("Network settings saved");
      }
    });
  };

  const onSubmitGate = async (data: FieldValues) => {
    const settings = prepareSettings(data);
    const dirty = formState.dirtyFields;

    // These fields will prompt a confirm dialog, all else save immediately
    const criticalFields = [
      // Label is for the UI, key is the internal key of the field
      { label: "IPv4 mode", key: "ipv4_mode" },
      { label: "IPv6 mode", key: "ipv6_mode" },
    ] as { label: string; key: keyof NetworkSettings }[];

    const criticalChanged = criticalFields.some(field => dirty[field.key]);

    // If no critical fields are changed, save immediately
    if (!criticalChanged) return onSubmit(settings);

    const changes = new Set<{ label: string; from: string; to: string }>();
    criticalFields.forEach(field => {
      const { key, label } = field;
      if (dirty[key]) {
        const from = initialSettingsRef?.current?.[key] as string;
        const to = data[key] as string;
        changes.add({ label, from, to });
      }
    });

    setStagedSettings(settings);
    setCriticalChanges(Array.from(changes));
    setShowCriticalSettingsConfirm(true);
  };

  const ipv4mode = watch("ipv4_mode");
  const ipv6mode = watch("ipv6_mode");

  const onDhcpLeaseRenew = () => {
    send("renewDHCPLease", {}, (resp: any) => {
      if ("error" in resp) {
        notifications.error("Failed to renew lease: " + resp.error.message);
      } else {
        notifications.success("DHCP lease renewed");
      }
    });
  };

  return (
    <>
      <FormProvider {...formMethods}>
        <form onSubmit={handleSubmit(onSubmitGate)} className="space-y-4">
          <SettingsPageHeader
            title="Network"
            description="Configure the network settings for the device"
            action={
              <>
                <div>
                  <Button
                    size="SM"
                    theme="primary"
                    disabled={!(formState.isDirty || formState.isSubmitting)}
                    loading={formState.isSubmitting}
                    type="submit"
                    text={formState.isSubmitting ? "Saving..." : "Save Settings"}
                  />
                </div>
              </>
            }
          />
          <div className="space-y-4">
            <SettingsItem
              title="MAC Address"
              description="Hardware identifier for the network interface"
            >
              <InputField
                type="text"
                size="SM"
                value={networkState?.mac_address}
                error={""}
                readOnly={true}
                className="dark:!text-opacity-60"
              />
            </SettingsItem>
            <SettingsItem title="Hostname" description="Set the device hostname">
              <InputField
                size="SM"
                placeholder="jetkvm"
                {...register("hostname")}
                error={formState.errors.hostname?.message}
              />
            </SettingsItem>
            <SettingsItem title="HTTP Proxy" description="Configure HTTP proxy settings">
              <InputField
                size="SM"
                placeholder="http://proxy.example.com:8080"
                {...register("http_proxy", {
                  validate: (value: string | null) => {
                    if (value === "" || value === null) return true;
                    if (!validator.isURL(value || "", { protocols: ["http", "https"] })) {
                      return "Invalid HTTP proxy URL";
                    }
                    return true;
                  },
                })}
                error={formState.errors.http_proxy?.message}
              />
            </SettingsItem>
            <div className="space-y-1">
              <SettingsItem
                title="Domain"
                description="Network domain suffix for the device"
              >
                <div className="space-y-2">
                  <SelectMenuBasic
                    size="SM"
                    options={[
                      { value: "dhcp", label: "DHCP provided" },
                      { value: "local", label: ".local" },
                      { value: "custom", label: "Custom" },
                    ]}
                    {...register("domain")}
                    error={formState.errors.domain?.message}
                  />
                </div>
              </SettingsItem>
              {watch("domain") === "custom" && (
                <div className="mt-2 w-1/3 border-l border-slate-800/10 pl-4 dark:border-slate-300/20">
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label="Custom Domain"
                    placeholder="home"
                    onChange={e => {
                      setCustomDomain(e.target.value);
                    }}
                  />
                </div>
              )}
            </div>

            <SettingsItem title="mDNS Mode" description="Configure mDNS settings">
              <SelectMenuBasic
                size="SM"
                options={[
                  { value: "disabled", label: "Disabled" },
                  { value: "auto", label: "Auto" },
                  { value: "ipv4_only", label: "IPv4 only" },
                  { value: "ipv6_only", label: "IPv6 only" },
                ]}
                {...register("mdns_mode")}
              />
            </SettingsItem>
            <SettingsItem
              title="Time synchronization"
              description="Configure time synchronization settings"
            >
              <SelectMenuBasic
                size="SM"
                options={[
                  { value: "ntp_only", label: "NTP only" },
                  { value: "ntp_and_http", label: "NTP and HTTP" },
                  { value: "http_only", label: "HTTP only" },
                ]}
                {...register("time_sync_mode")}
              />
            </SettingsItem>

            <SettingsItem title="IPv4 Mode" description="Configure the IPv4 mode">
              <SelectMenuBasic
                size="SM"
                options={[
                  { value: "dhcp", label: "DHCP" },
                  { value: "static", label: "Static" },
                ]}
                {...register("ipv4_mode")}
              />
            </SettingsItem>
            <div>
              <AutoHeight>
                {formState.isLoading ? (
                  <GridCard>
                    <div className="p-4">
                      <div className="space-y-4">
                        <div className="h-6 w-1/3 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
                        <div className="animate-pulse space-y-2">
                          <div className="h-4 w-1/4 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/4 rounded bg-slate-200 dark:bg-slate-700" />
                        </div>
                      </div>
                    </div>
                  </GridCard>
                ) : ipv4mode === "static" ? (
                  <StaticIpv4Card />
                ) : ipv4mode === "dhcp" && !!formState.dirtyFields.ipv4_mode ? (
                  <EmptyCard
                    IconElm={LuEthernetPort}
                    headline="Pending DHCP IPv4 mode change"
                    description="Save settings to enable DHCP mode and view lease information"
                  />
                ) : ipv4mode === "dhcp" ? (
                  <DhcpLeaseCard
                    networkState={networkState}
                    setShowRenewLeaseConfirm={setShowRenewLeaseConfirm}
                  />
                ) : (
                  <EmptyCard
                    IconElm={LuEthernetPort}
                    headline="Network Information"
                    description="No network configuration available"
                  />
                )}
              </AutoHeight>
            </div>

            <SettingsItem title="IPv6 Mode" description="Configure the IPv6 mode">
              <SelectMenuBasic
                size="SM"
                options={[
                  { value: "slaac", label: "SLAAC" },
                  { value: "static", label: "Static" },
                ]}
                {...register("ipv6_mode")}
              />
            </SettingsItem>
            <div className="space-y-4">
              <AutoHeight>
                {!networkState ? (
                  <GridCard>
                    <div className="p-4">
                      <div className="space-y-4">
                        <h3 className="text-base font-bold text-slate-900 dark:text-white">
                          IPv6 Network Information
                        </h3>
                        <div className="animate-pulse space-y-3">
                          <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                          <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                        </div>
                      </div>
                    </div>
                  </GridCard>
                ) : ipv6mode === "static" ? (
                  <StaticIpv6Card />
                ) : (
                  <Ipv6NetworkCard networkState={networkState || undefined} />
                )}
              </AutoHeight>
            </div>
            <>
              <div className="animate-fadeInStill animation-duration-300">
                <Button
                  size="SM"
                  theme="primary"
                  disabled={!(formState.isDirty || formState.isSubmitting)}
                  loading={formState.isSubmitting}
                  type="submit"
                  text={formState.isSubmitting ? "Saving..." : "Save Settings"}
                />
              </div>
            </>
          </div>
        </form>
      </FormProvider>

      {/* Critical change confirm */}
      <ConfirmDialog
        open={showCriticalSettingsConfirm}
        title="Apply network settings"
        variant="warning"
        confirmText="Apply changes"
        onConfirm={() => {
          setShowCriticalSettingsConfirm(false);
          if (stagedSettings) onSubmit(stagedSettings);

          // Wait for the close animation to finish before resetting the staged settings
          setTimeout(() => {
            setStagedSettings(null);
            setCriticalChanges([]);
          }, 500);
        }}
        onClose={() => {
          // close();
          setShowCriticalSettingsConfirm(false);
        }}
        isConfirming={formState.isSubmitting}
        description={
          <div className="space-y-4">
            <p>
              This will update the device&apos;s network configuration and may briefly
              disconnect your session.
            </p>

            <div className="rounded-md border border-slate-200 bg-slate-50 p-3 dark:border-slate-700 dark:bg-slate-900/40">
              <div className="mb-2 text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
                Pending changes
              </div>
              <dl className="grid grid-cols-1 gap-y-2">
                {criticalChanges.map((c, idx) => (
                  <div key={idx} className="w-full not-last:pb-2">
                    <div className="flex items-center gap-2 gap-x-8">
                      <dt className="text-sm text-slate-500 dark:text-slate-400">
                        {c.label}
                      </dt>
                      <div className="flex items-center gap-2">
                        <span className="rounded-sm bg-slate-200 px-1.5 py-0.5 text-sm font-medium text-slate-900 dark:bg-slate-700 dark:text-slate-100">
                          {c.from || "—"}
                        </span>

                        <span className="text-sm text-slate-500 dark:text-slate-400">
                          →
                        </span>

                        <span className="rounded-sm bg-slate-200 px-1.5 py-0.5 text-sm font-medium text-slate-900 dark:bg-slate-700 dark:text-slate-100">
                          {c.to}
                        </span>
                      </div>
                    </div>
                  </div>
                ))}
              </dl>
            </div>

            <p className="text-sm">
              If the network settings are invalid,{" "}
              <strong>the device may become unreachable</strong> and require a factory
              reset to restore connectivity.
            </p>
          </div>
        }
      />
      <ConfirmDialog
        open={showRenewLeaseConfirm}
        title="Renew DHCP Lease"
        variant="warning"
        confirmText="Renew Lease"
        description={
          <p>
            This will request a new IP address from your router. The device may briefly
            disconnect during the renewal process.
            <br />
            <br />
            If you receive a new IP address,{" "}
            <strong>you may need to reconnect using the new address</strong>.
          </p>
        }
        onConfirm={() => {
          setShowRenewLeaseConfirm(false);
          onDhcpLeaseRenew();
        }}
        onClose={() => setShowRenewLeaseConfirm(false)}
      />
    </>
  );
}
