
import { useCallback, useEffect, useRef, useState } from "react";
import { FieldValues, FormProvider, useForm } from "react-hook-form";
import { LuCopy, LuEthernetPort } from "react-icons/lu";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import validator from "validator";

import PublicIPCard from "@components/PublicIPCard";
import { NetworkSettings, NetworkState, useNetworkStateStore, useRTCStore } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";
import AutoHeight from "@components/AutoHeight";
import { Button } from "@components/Button";
import { ConfirmDialog } from "@components/ConfirmDialog";
import DhcpLeaseCard from "@components/DhcpLeaseCard";
import EmptyCard from "@components/EmptyCard";
import { GridCard } from "@components/Card";
import InputField, { InputFieldWithLabel } from "@components/InputField";
import Ipv6NetworkCard from "@components/Ipv6NetworkCard";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import StaticIpv4Card from "@components/StaticIpv4Card";
import StaticIpv6Card from "@components/StaticIpv6Card";
import { useCopyToClipboard } from "@components/useCopyToClipBoard";
import { netMaskFromCidr4 } from "@/utils/ip";
import { getNetworkSettings, getNetworkState } from "@/utils/jsonrpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages";

dayjs.extend(relativeTime);

const isLLDPAvailable = false; // LLDP is not supported yet

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

export function LifeTimeLabel({ lifetime }: Readonly<{ lifetime: string }>) {
  const [remaining, setRemaining] = useState<string | null>(null);

  // rrecalculate remaining time every 30 seconds
  useEffect(() => {
    // schedule immediate initial update
    setInterval(() => setRemaining(dayjs(lifetime).fromNow()), 0);

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  if (lifetime == "") {
    return <strong>{m.not_applicable()}</strong>;
  }

  return (
    <>
      <span className="text-sm font-medium">{remaining && <> {remaining}</>}</span>
      <span className="text-xs text-slate-700 dark:text-slate-300">&nbsp;({dayjs(lifetime).format("YYYY-MM-DD HH:mm")})</span>
    </>
  );
}

const NonCustomDomainOptions = ["dhcp", "local"];

export default function SettingsNetworkRoute() {
  const { send } = useJsonRpc();

  const networkState = useNetworkStateStore(state => state);
  const setNetworkState = useNetworkStateStore(state => state.setNetworkState);

  // Some input needs direct state management. Mostly options that open more details
  const [customDomain, setCustomDomain] = useState<string>("");

  // Confirm dialog
  const [showRenewLeaseConfirm, setShowRenewLeaseConfirm] = useState(false);

  // We use this to determine whether the settings have changed
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

      setNetworkState(state);

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

      if (!NonCustomDomainOptions.includes(settingsWithDefaults.domain)) {
        setCustomDomain(settingsWithDefaults.domain);
        settingsWithDefaults.domain = "custom";
      }

      initialSettingsRef.current = settingsWithDefaults;
      return { settings: settingsWithDefaults, state };
    } catch (err) {
      notifications.error(m.network_settings_load_error({ error: err instanceof Error ? err.message : m.unknown_error() }));
      throw err;
    }
  }, [setNetworkState, setCustomDomain]);

  const formMethods = useForm<NetworkSettings>({
    mode: "onBlur",

    defaultValues: async () => {
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
      const cidrNotation = Number.parseInt(parts[1]);
      if (Number.isNaN(cidrNotation) || cidrNotation < 0 || cidrNotation > 32) {
        return notifications.error(m.network_ipv4_invalid_cidr());
      }
      settings.ipv4_static.netmask = netMaskFromCidr4(cidrNotation);
      settings.ipv4_static.address = parts[0];
    }

    send("setNetworkSettings", { settings }, async (resp) => {
      if ("error" in resp) {
        notifications.error(m.network_save_settings_failed({ error: resp.error.message || m.unknown_error() }));
      } else {
        // If the settings are saved successfully, fetch the latest network data and reset the form
        // We do this so we get all the form state values, for stuff like is the form dirty, etc...

        try {
          const networkData = await fetchNetworkData();
          if (!networkData) return

          reset(networkData.settings);
          notifications.success(m.network_save_settings_success());

        } catch (error) {
          console.error("Failed to fetch network data:", error);
        }
        notifications.success(m.network_dhcp_lease_renew_success());
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
        label: m.network_dhcp_client_title(),
        from: initialSettingsRef.current?.dhcp_client as string,
        to: data.dhcp_client as string,
      });
    }

    if (dirty.ipv4_mode) {
      changes.push({
        label: m.network_ipv4_mode_title(),
        from: initialSettingsRef.current?.ipv4_mode as string,
        to: data.ipv4_mode as string,
      });
    }

    if (dirty.ipv4_static?.address) {
      changes.push({
        label: m.network_ipv4_address(),
        from: initialSettingsRef.current?.ipv4_static?.address as string,
        to: data.ipv4_static?.address as string,
      });
    }

    if (dirty.ipv4_static?.netmask) {
      changes.push({
        label: m.network_ipv4_netmask(),
        from: initialSettingsRef.current?.ipv4_static?.netmask as string,
        to: data.ipv4_static?.netmask as string,
      });
    }

    if (dirty.ipv4_static?.gateway) {
      changes.push({
        label: m.network_ipv4_gateway(),
        from: initialSettingsRef.current?.ipv4_static?.gateway as string,
        to: data.ipv4_static?.gateway as string,
      });
    }

    if (dirty.ipv4_static?.dns) {
      changes.push({
        label: m.network_ipv4_dns(),
        from: initialSettingsRef.current?.ipv4_static?.dns.join(", ").toString() ?? "",
        to: data.ipv4_static?.dns.join(", ").toString() ?? "",
      });
    }

    if (dirty.ipv6_mode) {
      changes.push({
        label: m.network_ipv6_mode_title(),
        from: initialSettingsRef.current?.ipv6_mode as string,
        to: data.ipv6_mode as string,
      });
    }

    if (dirty.ipv6_static?.prefix) {
      changes.push({
        label: m.network_ipv6_prefix(),
        from: initialSettingsRef.current?.ipv6_static?.prefix as string,
        to: data.ipv6_static?.prefix as string,
      });
    }

    if (dirty.ipv6_static?.gateway) {
      changes.push({
        label: m.network_ipv6_gateway(),
        from: initialSettingsRef.current?.ipv6_static?.gateway as string,
        to: data.ipv6_static?.gateway as string,
      });
    }

    if (dirty.ipv6_static?.dns) {
      changes.push({
        label: m.network_ipv6_dns(),
        from: initialSettingsRef.current?.ipv6_static?.dns.join(", ").toString() ?? "",
        to: data.ipv6_static?.dns.join(", ").toString() ?? "",
      });
    }

    if (dirty.hostname) {
      changes.push({
        label: m.network_hostname_title(),
        from: initialSettingsRef.current?.hostname?.toString() ?? "",
        to: data.hostname?.toString() ?? "",
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
        notifications.error(m.network_dhcp_lease_renew_failed({ error: resp.error.message || m.unknown_error() }));
      } else {
        notifications.success(m.network_dhcp_lease_renew_success());
      }
    });
  };

  const { copy } = useCopyToClipboard();

  return (
    <>
      <FormProvider {...formMethods}>
        <form onSubmit={handleSubmit(onSubmitGate)} className="space-y-4">
          <SettingsPageHeader
            title={m.network_title()}
            description={m.network_description()}
            action={
              <div>
                <Button
                  size="SM"
                  theme="primary"
                  disabled={!(formState.isDirty || formState.isSubmitting)}
                  loading={formState.isSubmitting}
                  type="submit"
                  text={formState.isSubmitting ? m.saving() : m.network_save_settings()}
                />
              </div>
            }
          />
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <SettingsItem
                title={m.network_mac_address_title()}
                description={m.network_mac_address_description()}
              />
              <div className="flex items-center">
                <GridCard cardClassName="rounded-r-none">
                  <div className=" h-[34px] flex items-center text-xs select-all text-black font-mono dark:text-white px-3 ">
                    {networkState?.mac_address} {" "}
                  </div>
                </GridCard>
                <Button className="rounded-l-none border-l-slate-800/30 dark:border-slate-300/20" size="SM" type="button" theme="light" LeadingIcon={LuCopy} onClick={async () => {
                  const mac = networkState?.mac_address || "";
                  if (await copy(mac)) {
                    notifications.success((m.network_mac_address_copy_success({ mac: mac })));
                  } else {
                    notifications.error(m.network_mac_address_copy_error());
                  }
                }} />
              </div>
            </div>

            <div className="space-y-4">
              <SettingsItem title={m.network_hostname_title()} description={m.network_hostname_description()}>
                <InputField
                  size="SM"
                  placeholder={networkState?.hostname || "jetkvm"}
                  {...register("hostname")}
                  error={formState.errors.hostname?.message}
                />
              </SettingsItem>

              <SettingsItem title={m.network_http_proxy_title()} description={m.network_http_proxy_description()}>
                <InputField
                  size="SM"
                  placeholder="http://proxy.example.com:8080"
                  {...register("http_proxy", {
                    validate: (value: string | null) => {
                      if (value === "" || value === null) return true;
                      if (!validator.isURL(value || "", { protocols: ["http", "https"] })) {
                        return m.network_http_proxy_invalid();
                      }
                      return true;
                    },
                  })}
                  error={formState.errors.http_proxy?.message}
                />
              </SettingsItem>

              <div className="space-y-1">
                <SettingsItem
                  title={m.network_domain_title()}
                  description={m.network_domain_description()}
                >
                  <div className="space-y-2">
                    <SelectMenuBasic
                      size="SM"
                      options={[
                        { value: "dhcp", label: m.network_domain_dhcp_provided() },
                        { value: "local", label: m.network_domain_local() },
                        { value: "custom", label: m.network_domain_custom() },
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
                      label={m.network_custom_domain()}
                      placeholder="home.example.com"
                      value={customDomain}
                      onChange={e => {
                        setCustomDomain(e.target.value);
                      }}
                    />
                  </div>
                )}
              </div>

              <SettingsItem
                title={m.network_mdns_title()}
                description={m.network_mdns_description()}
              >
                <SelectMenuBasic
                  size="SM"
                  options={[
                    { value: "disabled", label: m.network_mdns_disabled() },
                    { value: "auto", label: m.network_mdns_auto() },
                    { value: "ipv4_only", label: m.network_mdns_ipv4_only() },
                    { value: "ipv6_only", label: m.network_mdns_ipv6_only() },
                  ]}
                  {...register("mdns_mode")}
                />
              </SettingsItem>

              <SettingsItem
                title={m.network_time_sync_title()}
                description={m.network_time_sync_description()}
              >
                <SelectMenuBasic
                  size="SM"
                  options={[
                    // { value: "auto", label: "Auto" },
                    { value: "ntp_only", label: m.network_time_sync_ntp_only() },
                    { value: "ntp_and_http", label: m.network_time_sync_ntp_and_http() },
                    { value: "http_only", label: m.network_time_sync_http_only() },
                    // { value: "custom", label: "Custom" },
                  ]}
                  {...register("time_sync_mode")}
                />
              </SettingsItem>

              <SettingsItem title={m.network_dhcp_client_title()} description={m.network_dhcp_client_description()}>
                <SelectMenuBasic
                  size="SM"
                  options={[
                    { value: "jetdhcpc", label: m.network_dhcp_client_jetkvm() },
                    { value: "udhcpc", label: "udhcpc" }, // do not localize
                  ]}
                  {...register("dhcp_client")}
                />
              </SettingsItem>

              <SettingsItem title={m.network_ipv4_mode_title()} description={m.network_ipv4_mode_description()}>
                <SelectMenuBasic
                  size="SM"
                  options={[
                    { value: "dhcp", label: m.network_ipv4_mode_dhcp() },
                    { value: "static", label: m.network_ipv4_mode_static() },
                  ]}
                  {...register("ipv4_mode")}
                />
              </SettingsItem>

              <PublicIPCard />

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
                      headline={m.network_pending_dhcp_mode_change_headline()}
                      description={m.network_pending_dhcp_mode_change_description()}
                    />
                  ) : ipv4mode === "dhcp" ? (
                    <DhcpLeaseCard
                      networkState={networkState}
                      setShowRenewLeaseConfirm={setShowRenewLeaseConfirm}
                    />
                  ) : (
                    <EmptyCard
                      IconElm={LuEthernetPort}
                      headline={m.network_no_information_headline()}
                      description={m.network_no_information_description()}
                    />
                  )}
                </AutoHeight>
              </div>

              <SettingsItem title={m.network_ipv6_mode_title()} description={m.network_ipv6_mode_description()}>
                <SelectMenuBasic
                  size="SM"
                  options={[
                    //{ value: "disabled", label: m.network_ipv6_mode_disabled() },
                    { value: "slaac", label: m.network_ipv6_mode_slaac() },
                    //{ value: "dhcpv6", label: m.network_ipv6_mode_dhcpv6() },
                    //{ value: "slaac_and_dhcpv6", label: m.network_ipv6_mode_slaac_dhcpv6() },
                    { value: "static", label: m.network_ipv6_mode_static() },
                    { value: "link_local", label: m.network_ipv6_mode_link_local() },
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
                            {m.network_ipv6_information()}
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

              {isLLDPAvailable &&
                (
                  <div className="hidden space-y-4">
                    <SettingsItem
                      title={m.network_ll_dp_title()}
                      description={m.network_ll_dp_description()}
                    >
                      <SelectMenuBasic
                        size="SM"
                        options={[
                          { value: "disabled", label: m.network_ll_dp_disabled() },
                          { value: "basic", label: m.network_ll_dp_basic() },
                          { value: "all", label: m.network_ll_dp_all() },
                        ]}
                        {...register("lldp_mode")}
                      />
                    </SettingsItem>
                  </div>
                )
              }

              <div className="animate-fadeInStill animation-duration-300">
                <Button
                  size="SM"
                  theme="primary"
                  disabled={!(formState.isDirty || formState.isSubmitting)}
                  loading={formState.isSubmitting}
                  type="submit"
                  text={formState.isSubmitting ? m.saving() : m.network_save_settings()}
                />
              </div>
            </div>
          </div>
        </form>
      </FormProvider>

      {/* Critical change confirm */}
      <ConfirmDialog
        open={showCriticalSettingsConfirm}
        title={m.network_save_settings_apply_title()}
        variant="warning"
        confirmText={m.network_save_settings_confirm()}
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
          <div className="space-y-4" >
            <div>
              <p className="text-sm leading-relaxed text-slate-700 dark:text-slate-300">
                {m.network_save_settings_confirm_description()}
              </p>
            </div>

            <div className="space-y-2.5">
              <div className="flex items-center justify-between text-[13px] font-medium text-slate-900 dark:text-white">
                {m.network_save_settings_confirm_heading()}
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
                        {c.to || "—"}
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
        title={m.dhcp_lease_renew()}
        variant="warning"
        confirmText={m.network_dhcp_lease_renew_confirm()}
        description={
          <p>
            {m.network_dhcp_lease_renew_confirm_description()}
            <br />
            <br />
            {m.network_dhcp_lease_renew_confirm_new_a()}{" "}<strong>{m.network_dhcp_lease_renew_confirm_new_b()}</strong>.
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
