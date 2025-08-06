import { useCallback, useEffect, useState } from "react";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { LuEthernetPort } from "react-icons/lu";
import { useForm, FormProvider, FieldValues } from "react-hook-form";
import validator from "validator";

import { NetworkSettings, NetworkState, useRTCStore } from "@/hooks/stores";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField, { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import notifications from "@/notifications";
import { getNetworkSettings, getNetworkState } from "@/utils/jsonrpc";

import Ipv6NetworkCard from "../components/Ipv6NetworkCard";
import EmptyCard from "../components/EmptyCard";
import AutoHeight from "../components/AutoHeight";
import DhcpLeaseCard from "../components/DhcpLeaseCard";
import StaticIpv4Card from "../components/StaticIpv4Card";
import { useJsonRpc } from "../hooks/useJsonRpc";

import { SettingsItem } from "./devices.$id.settings";

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
  const [send] = useJsonRpc();

  const [networkState, setNetworkState] = useState<NetworkState | null>(null);

  // Some input needs direct state management. Mostly options that open more details
  const [customDomain, setCustomDomain] = useState<string>("");

  // Confirm dialog
  const [showRenewLeaseConfirm, setShowRenewLeaseConfirm] = useState(false);

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
      };

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

  const { register, handleSubmit, watch, formState, reset } = formMethods;

  const onSubmit = async (data: FieldValues) => {
    const settings = {
      ...data,

      // If custom domain option is selected, use the custom domain as value
      domain: data.domain === "custom" ? customDomain : data.domain,
      ipv4_static: {
        ...data.ipv4_static,

        // Remove empty DNS entries
        dns: data.ipv4_static?.dns.filter((dns: string) => dns.trim() !== ""),
      },
    };

    send("setNetworkSettings", { settings }, async resp => {
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

  const isIPv4Mode = watch("ipv4_mode");
  return (
    <>
      <FormProvider {...formMethods}>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <SettingsPageHeader
            title="Network"
            description="Configure the network settings for the device"
            action={
              <>
                {(formState.isDirty || formState.isSubmitting) && (
                  <div className="animate-fadeInStill opacity-0 animation-duration-300">
                    <Button
                      size="SM"
                      theme="primary"
                      disabled={formState.isSubmitting}
                      loading={formState.isSubmitting}
                      type="submit"
                      text={formState.isSubmitting ? "Saving..." : "Save Settings"}
                    />
                  </div>
                )}
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
                    if (value === "") return true;
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
                ) : isIPv4Mode === "static" ? (
                  <StaticIpv4Card />
                ) : isIPv4Mode === "dhcp" ? (
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
                options={[{ value: "slaac", label: "SLAAC" }]}
                {...register("ipv6_mode")}
              />
            </SettingsItem>
            <div className="space-y-4">
              <AutoHeight>
                {!networkState?.ipv6_addresses ? (
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
                ) : (
                  <Ipv6NetworkCard networkState={networkState || undefined} />
                )}
              </AutoHeight>
            </div>
            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            {(formState.isDirty || formState.isSubmitting) && (
              <div className="animate-fadeInStill opacity-0 animation-duration-300">
                <Button
                  size="SM"
                  theme="primary"
                  disabled={formState.isSubmitting}
                  loading={formState.isSubmitting}
                  type="submit"
                  text={formState.isSubmitting ? "Saving..." : "Save Settings"}
                />
              </div>
            )}
          </div>
        </form>
      </FormProvider>
      <ConfirmDialog
        open={showRenewLeaseConfirm}
        title="Renew DHCP Lease"
        description="Are you sure you want to renew the DHCP lease? This may temporarily disconnect the device."
        onConfirm={() => {
          setShowRenewLeaseConfirm(false);
        }}
        onClose={() => setShowRenewLeaseConfirm(false)}
      />
    </>
  );
}
