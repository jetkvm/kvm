import { LuPlus, LuX } from "react-icons/lu";
import { useFieldArray, useFormContext } from "react-hook-form";
import { useEffect } from "react";
import validator from "validator";

import { GridCard } from "@/components/Card";
import { Button } from "@/components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import { NetworkSettings } from "@/hooks/stores";

export default function StaticIpv4Card() {
  const formMethods = useFormContext<NetworkSettings>();
  const { register, formState, watch } = formMethods;

  const { fields, append, remove } = useFieldArray({ name: "ipv4_static.dns" });

  useEffect(() => {
    if (fields.length === 0) append("");
  }, [append, fields.length]);

  const dns = watch("ipv4_static.dns");

  const validate = (value: string) => {
    if (!validator.isIP(value)) return "Invalid IP address";
    return true;
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
              {...register("ipv4_static.address", { validate })}
              error={formState.errors.ipv4_static?.address?.message}
            />

            <InputFieldWithLabel
              label="Subnet Mask"
              type="text"
              size="SM"
              placeholder="255.255.255.0"
              {...register("ipv4_static.netmask", { validate })}
              error={formState.errors.ipv4_static?.netmask?.message}
            />
          </div>

          <InputFieldWithLabel
            label="Gateway"
            type="text"
            size="SM"
            placeholder="192.168.1.1"
            {...register("ipv4_static.gateway", { validate })}
            error={formState.errors.ipv4_static?.gateway?.message}
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
                        placeholder="1.1.1.1"
                        {...register(`ipv4_static.dns.${index}`, { validate })}
                        error={formState.errors.ipv4_static?.dns?.[index]?.message}
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
            disabled={dns[0] === ""}
          />
        </div>
      </div>
    </GridCard>
  );
}
