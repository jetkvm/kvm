import { LuPlus, LuX } from "react-icons/lu";
import { useFieldArray, useFormContext } from "react-hook-form";
import validator from "validator";
import { useEffect } from "react";

import { GridCard } from "@/components/Card";
import { Button } from "@/components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import { NetworkSettings } from "@/hooks/stores";

export default function StaticIpv6Card() {
  const formMethods = useFormContext<NetworkSettings>();
  const { register, formState, watch } = formMethods;

  const { fields, append, remove } = useFieldArray({ name: "ipv6_static.dns" });

  useEffect(() => {
    if (fields.length === 0) append("");
  }, [append, fields.length]);

  const dns = watch("ipv6_static.dns");

  const cidrValidation = (value: string) => {
    if (value === "") return true;

    // Check if it's a valid IPv6 address with CIDR notation
    const parts = value.split("/");
    if (parts.length !== 2) return "Please use CIDR notation (e.g., 2001:db8::1/64)";

    const [address, prefix] = parts;
    if (!validator.isIP(address, 6)) return "Invalid IPv6 address";
    const prefixNum = parseInt(prefix);
    if (isNaN(prefixNum) || prefixNum < 0 || prefixNum > 128) {
      return "Prefix must be between 0 and 128";
    }

    return true;
  };

  const ipv6Validation = (value: string) => {
    if (!validator.isIP(value, 6)) return "Invalid IPv6 address";
    return true;
  };

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            Static IPv6 Configuration
          </h3>

          <InputFieldWithLabel
            label="IP Prefix"
            type="text"
            size="SM"
            placeholder="2001:db8::1/64"
            {...register("ipv6_static.prefix", { validate: (value: string | undefined) => cidrValidation(value ?? "") })}
            error={formState.errors.ipv6_static?.prefix?.message}
          />

          <InputFieldWithLabel
            label="Gateway"
            type="text"
            size="SM"
            placeholder="2001:db8::1"
            {...register("ipv6_static.gateway", { validate: (value: string | undefined) => ipv6Validation(value ?? "") })}
            error={formState.errors.ipv6_static?.gateway?.message}
          />

          {/* DNS server fields */}
          <div className="space-y-4">
            {fields.map((dns, index) => {
              return (
                <div key={dns.id}>
                  <div className="flex items-start gap-x-2">
                    <div className="flex-1">
                      <InputFieldWithLabel
                        label={index === 0 ? "DNS Server" : null}
                        type="text"
                        size="SM"
                        placeholder="2001:4860:4860::8888"
                        {...register(`ipv6_static.dns.${index}`, { validate: (value: string | undefined) => ipv6Validation(value ?? "") })}
                        error={formState.errors.ipv6_static?.dns?.[index]?.message}
                      />
                    </div>
                    {index > 0 && (
                      <div className="flex-shrink-0">
                        <Button
                          size="SM"
                          theme="light"
                          type="button"
                          onClick={() => remove(index)}
                          LeadingIcon={LuX}
                        />
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          <Button
            size="SM"
            theme="light"
            onClick={() => append("", { shouldFocus: true })}
            LeadingIcon={LuPlus}
            type="button"
            text="Add DNS Server"
            disabled={dns?.[0] === ""}
          />
        </div>
      </div>
    </GridCard>
  );
}
