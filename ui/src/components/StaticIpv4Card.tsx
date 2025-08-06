import { useState, useEffect } from "react";
import { LuPlus, LuX } from "react-icons/lu";
import isIP from "validator/es/lib/isIP";

import { GridCard } from "@/components/Card";
import { Button } from "@/components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import { IPv4StaticConfig, NetworkSettings, NetworkState } from "@/hooks/stores";

interface StaticIpv4CardProps {
  networkSettings: NetworkSettings;
  onUpdate: (settings: NetworkSettings) => void;
  networkState?: NetworkState;
  onApply: () => void;
}

export default function StaticIpv4Card({
  networkSettings,
  onUpdate,
  networkState,
  onApply,
}: StaticIpv4CardProps) {
  const [staticConfig, setStaticConfig] = useState<IPv4StaticConfig>({
    address: "",
    netmask: "",
    gateway: "",
    dns: [],
  });

  // Validation errors
  const addressError =
    staticConfig.address && !isIP(staticConfig.address, "4") ? "Invalid IP address" : "";
  const netmaskError =
    staticConfig.netmask && !isIP(staticConfig.netmask, "4") ? "Invalid subnet mask" : "";
  const gatewayError =
    staticConfig.gateway && !isIP(staticConfig.gateway, "4")
      ? "Invalid gateway address"
      : "";
  const dnsErrors = staticConfig.dns.map(dns =>
    dns && !isIP(dns, "4") ? "Invalid DNS server" : "",
  );

  // Check if any field has an error or if required fields are empty
  const hasValidationErrors = !!(
    addressError ||
    netmaskError ||
    gatewayError ||
    dnsErrors.some(error => error)
  );
  const hasEmptyRequiredFields =
    !staticConfig.address ||
    !staticConfig.netmask ||
    !staticConfig.gateway ||
    staticConfig.dns.length === 0;
  const isFormValid = !hasValidationErrors && !hasEmptyRequiredFields;

  // Initialize from existing settings or use current network state as defaults
  useEffect(() => {
    if (networkSettings.ipv4_static) {
      setStaticConfig(networkSettings.ipv4_static);
    } else if (networkState?.dhcp_lease) {
      // Use current DHCP values as defaults
      const defaults: IPv4StaticConfig = {
        address: networkState.dhcp_lease.ip || "",
        netmask: networkState.dhcp_lease.netmask || "",
        gateway: networkState.dhcp_lease.routers?.[0] || "",
        dns: networkState.dhcp_lease.dns_servers || [],
      };
      setStaticConfig(defaults);
      // Update the parent with these default values
      onUpdate({ ...networkSettings, ipv4_static: defaults });
    }
  }, [networkSettings.ipv4_static, networkState?.dhcp_lease, networkSettings, onUpdate]);

  const handleConfigChange = (
    field: keyof Omit<IPv4StaticConfig, "dns">,
    value: string,
  ) => {
    const updatedConfig = { ...staticConfig, [field]: value };
    setStaticConfig(updatedConfig);
    onUpdate({ ...networkSettings, ipv4_static: updatedConfig });
  };

  const handleDnsChange = (index: number, value: string) => {
    const updatedDns = [...staticConfig.dns];
    updatedDns[index] = value;
    const updatedConfig = { ...staticConfig, dns: updatedDns };
    setStaticConfig(updatedConfig);
    onUpdate({ ...networkSettings, ipv4_static: updatedConfig });
  };

  const handleAddDnsField = () => {
    const updatedConfig = {
      ...staticConfig,
      dns: [...staticConfig.dns, ""],
    };
    setStaticConfig(updatedConfig);
    onUpdate({ ...networkSettings, ipv4_static: updatedConfig });
  };

  const handleRemoveDnsServer = (index: number) => {
    const updatedConfig = {
      ...staticConfig,
      dns: staticConfig.dns.filter((_, i) => i !== index),
    };
    setStaticConfig(updatedConfig);
    onUpdate({ ...networkSettings, ipv4_static: updatedConfig });
  };

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            Static IPv4 Configuration
          </h3>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <InputFieldWithLabel
              label="IP Address"
              type="text"
              size="SM"
              placeholder="192.168.1.100"
              value={staticConfig.address}
              onChange={e => handleConfigChange("address", e.target.value)}
              error={addressError}
            />

            <InputFieldWithLabel
              label="Subnet Mask"
              type="text"
              size="SM"
              placeholder="255.255.255.0"
              value={staticConfig.netmask}
              onChange={e => handleConfigChange("netmask", e.target.value)}
              error={netmaskError}
            />
          </div>

          <InputFieldWithLabel
            label="Gateway"
            type="text"
            size="SM"
            placeholder="192.168.1.1"
            value={staticConfig.gateway}
            onChange={e => handleConfigChange("gateway", e.target.value)}
            error={gatewayError}
          />

          {/* DNS server fields */}
          <div className="space-y-2">
            {staticConfig.dns.length === 0 && (
              <div className="flex items-center gap-2">
                <div className="flex-1">
                  <InputFieldWithLabel
                    label="Primary DNS Server"
                    type="text"
                    size="SM"
                    placeholder="8.8.8.8"
                    value=""
                    onChange={e => {
                      const updatedConfig = { ...staticConfig, dns: [e.target.value] };
                      setStaticConfig(updatedConfig);
                      onUpdate({ ...networkSettings, ipv4_static: updatedConfig });
                    }}
                  />
                </div>
              </div>
            )}

            {staticConfig.dns.map((dns, index) => (
              <StaticIpv4DnsField
                key={index}
                value={dns}
                index={index}
                isLast={index === staticConfig.dns.length - 1}
                onChange={e => handleDnsChange(index, e)}
                onAdd={handleAddDnsField}
                onRemove={handleRemoveDnsServer}
                error={dnsErrors[index]}
              />
            ))}
          </div>
        </div>
      </div>
    </GridCard>
  );
}

export function StaticIpv4DnsField({
  value,
  index,
  isLast,
  onChange,
  onAdd,
  onRemove,
  error,
}: {
  value: string;
  index: number;
  isLast: boolean;
  onChange: (dns: string) => void;
  onAdd: () => void;
  onRemove: (index: number) => void;
  error?: string;
}) {
  return (
    <div key={index} className="flex items-center gap-2">
      <div className="flex-1">
        <InputFieldWithLabel
          label={index === 0 ? "Primary DNS Server" : `DNS Server ${index + 1}`}
          type="text"
          size="SM"
          placeholder="8.8.8.8"
          value={value}
          onChange={e => onChange(e.target.value)}
          error={error || ""}
        />
      </div>
      <div className="mt-[21.875px] flex-shrink-0">
        {
          // if last item, show add button
          isLast ? (
            <Button size="SM" theme="light" onClick={onAdd} LeadingIcon={LuPlus} />
          ) : (
            <Button
              size="SM"
              theme="danger"
              onClick={() => onRemove(index)}
              LeadingIcon={LuX}
            />
          )
        }
      </div>
    </div>
  );
}
