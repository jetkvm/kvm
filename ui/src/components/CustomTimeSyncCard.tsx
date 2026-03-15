import { useEffect } from "react";
import { LuPlus, LuX } from "react-icons/lu";
import { useFieldArray, useFormContext } from "react-hook-form";
import validator from "validator";

import { NetworkSettings } from "@hooks/stores";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField from "@components/InputField";
import FieldLabel from "@components/FieldLabel";
import { m } from "@localizations/messages.js";

const isValidNtpServer = (value: string): boolean => {
  if (validator.isIP(value)) return true;
  if (validator.isFQDN(value)) return true;
  return false;
};

export default function CustomTimeSyncCard() {
  const { register, formState, watch } = useFormContext<NetworkSettings>();

  const {
    fields: ntpFields,
    append: ntpAppend,
    remove: ntpRemove,
  } = useFieldArray({ name: "time_sync_ntp_servers" });

  const {
    fields: httpFields,
    append: httpAppend,
    remove: httpRemove,
  } = useFieldArray({ name: "time_sync_http_urls" });

  const ntpServers = watch("time_sync_ntp_servers");
  const httpUrls = watch("time_sync_http_urls");

  useEffect(() => {
    if (ntpFields.length === 0) ntpAppend("");
  }, [ntpAppend, ntpFields.length]);

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            {m.network_time_sync_config_header()}
          </h3>

          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            {/* NTP servers */}
            <div className="space-y-3">
              <FieldLabel label={m.network_time_sync_user_ntp_servers_label()} />
              {ntpFields.map((field, index) => (
                <div key={field.id} className="flex items-start gap-x-2">
                  <div className="flex-1">
                    <InputField
                      type="text"
                      size="SM"
                      placeholder="pool.ntp.org"
                      {...register(`time_sync_ntp_servers.${index}`, {
                        validate: (value: string | undefined) => {
                          if (!value || !isValidNtpServer(value)) {
                            return m.network_time_sync_ntp_server_invalid();
                          }
                          return true;
                        },
                      })}
                      error={formState.errors.time_sync_ntp_servers?.[index]?.message}
                    />
                  </div>
                  {index > 0 && (
                    <div className="shrink-0">
                      <Button
                        size="SM"
                        theme="light"
                        type="button"
                        onClick={() => ntpRemove(index)}
                        LeadingIcon={LuX}
                      />
                    </div>
                  )}
                </div>
              ))}
              <Button
                size="SM"
                theme="light"
                onClick={() => ntpAppend("", { shouldFocus: true })}
                LeadingIcon={LuPlus}
                type="button"
                text={m.network_time_sync_add_ntp_server()}
                disabled={!ntpServers?.every(v => v?.length > 0)}
              />
            </div>

            {/* HTTP URLs */}
            <div className="space-y-3">
              <FieldLabel label={m.network_time_sync_user_http_urls_label()} />
              {httpFields.map((field, index) => (
                <div key={field.id} className="flex items-start gap-x-2">
                  <div className="flex-1">
                    <InputField
                      type="text"
                      size="SM"
                      placeholder="http://www.gstatic.com/generate_204"
                      {...register(`time_sync_http_urls.${index}`, {
                        validate: (value: string | undefined) => {
                          if (
                            !value ||
                            !validator.isURL(value, {
                              protocols: ["http", "https"],
                              require_protocol: true,
                            })
                          ) {
                            return m.network_time_sync_http_url_invalid();
                          }
                          return true;
                        },
                      })}
                      error={formState.errors.time_sync_http_urls?.[index]?.message}
                    />
                  </div>
                  <div className="shrink-0">
                    <Button
                      size="SM"
                      theme="light"
                      type="button"
                      onClick={() => httpRemove(index)}
                      LeadingIcon={LuX}
                    />
                  </div>
                </div>
              ))}
              <Button
                size="SM"
                theme="light"
                onClick={() => httpAppend("", { shouldFocus: true })}
                LeadingIcon={LuPlus}
                type="button"
                text={m.network_time_sync_add_http_url()}
                disabled={httpUrls && httpUrls.length > 0 && !httpUrls.every(v => v?.length > 0)}
              />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
