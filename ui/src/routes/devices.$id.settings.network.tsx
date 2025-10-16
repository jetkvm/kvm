import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { useCallback, useEffect, useRef, useState } from "react";
import { FieldValues, FormProvider, useForm } from "react-hook-form";
import { LuCopy, LuEthernetPort } from "react-icons/lu";
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
import { netMaskFromCidr4 } from "@/utils/ip";

import AutoHeight from "../components/AutoHeight";
import DhcpLeaseCard from "../components/DhcpLeaseCard";
import EmptyCard from "../components/EmptyCard";
import Ipv6NetworkCard from "../components/Ipv6NetworkCard";
import StaticIpv4Card from "../components/StaticIpv4Card";
import StaticIpv6Card from "../components/StaticIpv6Card";
import { useJsonRpc } from "../hooks/useJsonRpc";
import { SettingsItem } from "../components/SettingsItem";
import { useCopyToClipboard } from "../components/useCopyToClipBoard";

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
  }, [setNetworkState]);

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

  const prepareSettings = useCallback((data: FieldValues) => {
    return {
      ...data,

      // If custom domain option is selected, use the custom domain as value
      domain: data.domain === "custom" ? customDomain : data.domain,
    } as NetworkSettings;
  }, [customDomain]);

  const { register, handleSubmit, watch, formState, reset } = formMethods;

  const onSubmit = useCallback(async (settings: NetworkSettings) => {
    if (settings.ipv4_static?.address?.includes("/")) {
      const parts = settings.ipv4_static.address.split("/");
      const cidrNotation = parseInt(parts[1]);
      if (isNaN(cidrNotation) || cidrNotation < 0 || cidrNotation > 32) {
        return notifications.error("Invalid CIDR notation for IPv4 address");
      }
      settings.ipv4_static.netmask = netMaskFromCidr4(cidrNotation);
      settings.ipv4_static.address = parts[0];
    }

    send("setNetworkSettings", { settings }, async (resp) => {
      if ("error" in resp) {
        return notifications.error(
          resp.error.data ? resp.error.data : resp.error.message,
        );
      } else {
        // If the settings are saved successfully, fetch the latest network data and reset the form
        // We do this so we get all the form state values, for stuff like is the form dirty, etc...

        try {
          const networkData = await fetchNetworkData();
          if (!networkData) return

          reset(networkData.settings);
          notifications.success("Network settings saved");

        } catch (error) {
          console.error("Failed to fetch network data:", error);
        }
      }
    });
  }, [fetchNetworkData, reset, send]);

  const onSubmitGate = useCallback(async (data: FieldValues) => {
    const settings = prepareSettings(data);
    const dirty = formState.dirtyFields;

    // Build list of critical changes for display
    const changes: { label: string; from: string; to: string }[] = [];

    if (dirty.dhcp_client) {
      changes.push({
        label: "DHCP client",
        from: initialSettingsRef.current?.dhcp_client as string,
        to: data.dhcp_client as string,
      });
    }

    if (dirty.ipv4_mode) {
      changes.push({
        label: "IPv4 mode",
        from: initialSettingsRef.current?.ipv4_mode as string,
        to: data.ipv4_mode as string,
      });
    }

    if (dirty.ipv4_static?.address) {
      changes.push({
        label: "IPv4 address",
        from: initialSettingsRef.current?.ipv4_static?.address as string,
        to: data.ipv4_static?.address as string,
      });
    }

    if (dirty.ipv4_static?.netmask) {
      changes.push({
        label: "IPv4 netmask",
        from: initialSettingsRef.current?.ipv4_static?.netmask as string,
        to: data.ipv4_static?.netmask as string,
      });
    }

    if (dirty.ipv4_static?.gateway) {
      changes.push({
        label: "IPv4 gateway",
        from: initialSettingsRef.current?.ipv4_static?.gateway as string,
        to: data.ipv4_static?.gateway as string,
      });
    }

    if (dirty.ipv4_static?.dns) {
      changes.push({
        label: "IPv4 DNS",
        from: initialSettingsRef.current?.ipv4_static?.dns.join(", ").toString() ?? "",
        to: data.ipv4_static?.dns.join(", ").toString() ?? "",
      });
    }

    if (dirty.ipv6_mode) {
      changes.push({
        label: "IPv6 mode",
        from: initialSettingsRef.current?.ipv6_mode as string,
        to: data.ipv6_mode as string,
      });
    }

    // If no critical fields are changed, save immediately
    if (changes.length === 0) return onSubmit(settings);

    // Show confirmation dialog for critical changes
    setStagedSettings(settings);
    setCriticalChanges(changes);
    setShowCriticalSettingsConfirm(true);
  }, [prepareSettings, formState.dirtyFields, onSubmit]);

  const ipv4mode = watch("ipv4_mode");
  const ipv6mode = watch("ipv6_mode");

  const onDhcpLeaseRenew = () => {
    send("renewDHCPLease", {}, (resp) => {
      if ("error" in resp) {
        notifications.error("Failed to renew lease: " + resp.error.message);
      } else {
        notifications.success("DHCP lease renewed");
      }
    });
  };

  const { copy } = useCopyToClipboard();

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
            <div className="flex items-center justify-between">
              <SettingsItem
                title="MAC Address"
                description="Hardware identifier for the network interface"
              />
              <div className="flex items-center">
                <GridCard cardClassName="rounded-r-none">
                  <div className=" h-[34px] flex items-center text-xs select-all text-black font-mono dark:text-white px-3 ">
                    {networkState?.mac_address} {" "}
                  </div>
                </GridCard>
                <Button className="rounded-l-none border-l-slate-800/30 dark:border-slate-300/20" size="SM" type="button" theme="light" LeadingIcon={LuCopy} onClick={async () => {
                  if (await copy(networkState?.mac_address || "")) {
                    notifications.success("MAC address copied to clipboard");
                  } else {
                    notifications.error("Failed to copy MAC address");
                  }
                }} />
              </div>
            </div>
            <SettingsItem title="Hostname" description="Set the device hostname">
              <InputField
                size="SM"
                placeholder={networkState?.hostname || "jetkvm"}
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

            <SettingsItem title="DHCP client" description="Configure which DHCP client to use">
              <SelectMenuBasic
                size="SM"
                options={[
                  { value: "jetdhcpc", label: "JetKVM" },
                  { value: "udhcpc", label: "udhcpc" },
                ]}
                {...register("dhcp_client")}
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
          setShowCriticalSettingsConfirm(false);
        }}
        isConfirming={formState.isSubmitting}
        description={
          <div className="space-y-4">
            <div>
              <p className="text-sm leading-relaxed text-slate-700 dark:text-slate-300">
                The following network settings will be applied. These changes may require a reboot and cause brief disconnection.
              </p>
            </div>

            <div className="space-y-2.5">
              <div className="flex items-center justify-between text-[13px] font-medium text-slate-900 dark:text-white">
                Configuration changes
              </div>
              <div className="space-y-2.5">
                {criticalChanges.map((c, idx) => (
                  <div key={idx + c.label} className="flex items-center gap-x-2 gap-y-1 flex-wrap bg-slate-100/50 dark:bg-slate-800/50 border border-slate-800/10 dark:border-slate-300/20 rounded-md py-2 px-3">
                    <span className="text-xs text-slate-600 dark:text-slate-400">{c.label}</span>
                    <div className="flex items-center gap-2.5">
                      <code className="rounded border border-slate-800/20 bg-slate-50 px-1.5 py-1 text-xs text-black font-mono dark:border-slate-300/20 dark:bg-slate-800 dark:text-slate-100">
                        {c.from || "—"}
                      </code>
                      <svg className="size-3.5 text-slate-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M13 7l5 5m0 0l-5 5m5-5H6" />
                      </svg>
                      <code className="rounded border border-slate-800/20 bg-slate-50 px-1.5 py-1 text-xs text-black font-mono dark:border-slate-300/20 dark:bg-slate-800 dark:text-slate-100">
                        {c.to}
                      </code>
                    </div>
                  </div>
                ))}
              </div>
            </div>

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
