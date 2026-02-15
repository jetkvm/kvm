import { useEffect } from "react";
import validator from "validator";
import { LuPlus, LuX } from "react-icons/lu";
import { useFieldArray, useFormContext } from "react-hook-form";

import { NetworkSettings } from "@hooks/stores";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { InputFieldWithLabel } from "@components/InputField";
import { m } from "@localizations/messages.js";

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
    if (parts.length !== 2) return m.network_ipv6_cidr_suggestion();

    const [address, prefix] = parts;
    if (!validator.isIP(address, 6)) return m.network_ipv6_invalid();
    const prefixNum = parseInt(prefix);
    if (isNaN(prefixNum) || prefixNum < 0 || prefixNum > 128) {
      return m.network_ipv6_prefix_invalid();
    }

    return true;
  };

  const ipv6Validation = (value: string) => {
    if (!validator.isIP(value, 6)) return m.network_ipv6_invalid();
    return true;
  };

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            {m.network_static_ipv6_header()}
          </h3>

          <InputFieldWithLabel
            label={m.network_ipv6_prefix()}
            type="text"
            size="SM"
            placeholder="2001:db8::1/64"
            {...register("ipv6_static.prefix", {
              validate: (value: string | undefined) => cidrValidation(value ?? ""),
            })}
            error={formState.errors.ipv6_static?.prefix?.message}
          />

          <InputFieldWithLabel
            label={m.network_ipv6_gateway()}
            type="text"
            size="SM"
            placeholder="2001:db8::1"
            {...register("ipv6_static.gateway", {
              validate: (value: string | undefined) => ipv6Validation(value ?? ""),
            })}
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
                        label={index === 0 ? m.network_ipv6_dns() : null}
                        type="text"
                        size="SM"
                        placeholder="2001:4860:4860::8888"
                        {...register(`ipv6_static.dns.${index}`, {
                          validate: (value: string | undefined) => ipv6Validation(value ?? ""),
                        })}
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
            text={m.network_settings_add_dns()}
            disabled={dns?.[0] === ""}
          />
        </div>
      </div>
    </GridCard>
  );
}
