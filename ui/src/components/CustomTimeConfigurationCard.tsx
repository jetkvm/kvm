import { useEffect, useMemo, useState } from "react";
import { LuPlus, LuX } from "react-icons/lu";
import { Controller, useFieldArray, useFormContext } from "react-hook-form";
import validator from "validator";

import { NetworkSettings } from "@hooks/stores";
import { Button } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import { Combobox, ComboboxOption } from "@components/Combobox";
import FieldLabel from "@components/FieldLabel";
import { GridCard } from "@components/Card";
import InputField, { FieldError } from "@components/InputField";
import { SettingsItem } from "@components/SettingsItem";
import { m } from "@localizations/messages.js";

const timeSourcesDisplayMap = {
  ntp: m.network_time_sync_source_builtin_ntp(),
  http: m.network_time_sync_source_builtin_http(),
  ntp_dhcp: m.network_time_sync_source_dhcp(),
  ntp_user_provided: m.network_time_sync_source_user_ntp(),
  http_user_provided: m.network_time_sync_source_user_http(),
} as Record<string, string>;

const ensureArray = <T,>(arr: T[] | null | undefined): T[] => {
  return Array.isArray(arr) ? arr : [];
};

const sourceDisplay = (sourceDisplayMap: Record<string, string>, source: string): string => {
  return sourceDisplayMap[source] || source;
};

export default function CustomTimeConfigurationCard() {
  const formMethods = useFormContext<NetworkSettings>();
  const { control, formState, register, trigger, watch } = formMethods;

  const [sourceQuery, setSourceQuery] = useState<string>("");

  const time_sync_ordering = watch("time_sync_ordering");
  const time_sync_ntp_servers = watch("time_sync_ntp_servers");
  const time_sync_http_urls = watch("time_sync_http_urls");
  const time_sync_disable_fallback = watch("time_sync_disable_fallback");
  const time_sync_parallel = watch("time_sync_parallel");

  const validateOrderingMethod = (list: string[] | null, source: string, err: string) => {
    if (time_sync_ordering?.includes(source)) {
      if (list === null || list?.length <= 0 || list?.every(v => v?.length <= 0)) {
        return err;
      }
    }
    return true;
  };

  const {
    fields: ntpFields,
    append: ntpAppend,
    replace: ntpReplace,
  } = useFieldArray({
    name: "time_sync_ntp_servers",
    rules: {
      validate: value => {
        return validateOrderingMethod(
          value as string[],
          "ntp_user_provided",
          m.network_time_sync_ntp_server_required(),
        );
      },
    },
  });

  const {
    fields: httpFields,
    append: httpAppend,
    replace: httpReplace,
  } = useFieldArray({
    name: "time_sync_http_urls",
    rules: {
      validate: value => {
        return validateOrderingMethod(
          value as string[],
          "http_user_provided",
          m.network_time_sync_http_url_required(),
        );
      },
    },
  });

  const sourceOptions = useMemo(
    () =>
      Object.keys(timeSourcesDisplayMap).map(key => ({
        value: key,
        label: sourceDisplay(timeSourcesDisplayMap, key),
      })),
    [],
  );

  const filteredSources = useMemo(() => {
    const selectedSources = ensureArray(time_sync_ordering);
    const availableSources = sourceOptions.filter(
      option => !selectedSources.includes(option.value),
    );

    if (sourceQuery === "") {
      return availableSources;
    } else {
      return availableSources.filter(option =>
        option.label.toLowerCase().includes(sourceQuery.toLowerCase()),
      );
    }
  }, [sourceOptions, sourceQuery, time_sync_ordering]);

  const onSourceSelect = (option: { value: string | null; sources?: string[] }) => {
    let sources: string[] = [];
    if (option.sources) {
      // they gave us a full set of keys (e.g. from deleting one)
      sources = [...new Set(ensureArray(option.sources))];
    } else {
      // they gave us a single key to add
      sources = [...time_sync_ordering];
      if (option.value !== null) {
        sources.push(option.value);
      }
      sources = [...new Set(sources)];
    }
    return sources;
  };

  const onSourceQueryChange = (query: string) => {
    setSourceQuery(query);
  };

  // If somehow, the saved config is invalid, show validation errors
  useEffect(() => {
    trigger();
  }, [trigger]);

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            {m.network_time_sync_config_header()}
          </h3>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-1">
            {/* Source ordering */}
            <div className="flex w-full flex-col gap-1">
              <div className="flex items-center gap-1">
                <FieldLabel
                  label={m.network_time_sync_ordering_title()}
                  description={m.network_time_sync_ordering_description()}
                />
              </div>
              <Controller
                name="time_sync_ordering"
                control={control}
                rules={{
                  validate: (value: string[] | null) => {
                    if (value === null || value?.length <= 0) {
                      return m.network_time_sync_ordering_required();
                    }
                    return true;
                  },
                }}
                render={({ field: orderingField, fieldState: orderingFieldState }) => (
                  <>
                    {orderingField?.value?.length > 0 && (
                      <div className="flex flex-wrap gap-1 pb-2">
                        {orderingField.value.map((source, sourceIndex) => (
                          <span
                            key={`source-${sourceIndex}`}
                            className="inline-flex items-center rounded-md bg-blue-100 px-1 py-0.5 text-xs font-medium text-blue-800 dark:bg-blue-900/40 dark:text-blue-200"
                          >
                            <span className="px-1">
                              {sourceDisplay(timeSourcesDisplayMap, source)}
                            </span>
                            <Button
                              size="XS"
                              className=""
                              theme="blank"
                              type="button"
                              onClick={async () => {
                                const newSources = time_sync_ordering.filter(
                                  (_, i) => i !== sourceIndex,
                                );
                                await orderingField.onChange(
                                  onSourceSelect({ value: null, sources: newSources }),
                                );
                                orderingField.onBlur();
                                trigger("time_sync_ntp_servers");
                                trigger("time_sync_http_urls");
                              }}
                              LeadingIcon={LuX}
                            />
                          </span>
                        ))}
                      </div>
                    )}
                    <div className="relative w-full">
                      <Combobox
                        onChange={async option => {
                          const selectedOption = option as ComboboxOption | null;
                          await orderingField.onChange(
                            onSourceSelect({ value: selectedOption?.value ?? null }),
                          );
                          onSourceQueryChange("");
                          orderingField.onBlur();
                          trigger("time_sync_ntp_servers");
                          trigger("time_sync_http_urls");
                        }}
                        displayValue={() => sourceQuery}
                        onInputChange={onSourceQueryChange}
                        options={() => filteredSources}
                        size="SM"
                        immediate
                        placeholder={m.network_time_sync_select_sources()}
                        emptyMessage={m.network_time_sync_no_matching_sources()}
                        error={orderingFieldState?.error?.message}
                      />
                    </div>
                  </>
                )}
              />
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {/* NTP server fields */}
            <div className="space-y-4">
              <FieldLabel label={m.network_time_sync_user_ntp_servers_label()} />
              {ntpFields.map((ntpServer, ntpIndex) => {
                return (
                  <div key={ntpServer.id}>
                    <div className="flex items-start gap-x-2">
                      <div className="flex-1">
                        <InputField
                          type="text"
                          size="SM"
                          placeholder="pool.ntp.org"
                          {...register(`time_sync_ntp_servers.${ntpIndex}`, {
                            validate: (value: string | null) => {
                              if (value === null || value === "") {
                                return m.network_time_sync_ntp_server_invalid();
                              }
                              return true;
                            },
                          })}
                          onBlur={() => {
                            trigger("time_sync_ntp_servers");
                            trigger("time_sync_ordering");
                          }}
                          error={formState.errors.time_sync_ntp_servers?.[ntpIndex]?.message}
                        />
                      </div>
                      {ntpIndex >= 0 && (
                        <div className="shrink-0">
                          <Button
                            size="SM"
                            theme="light"
                            type="button"
                            onClick={() => {
                              ntpReplace(time_sync_ntp_servers?.filter((_v, i) => i != ntpIndex));
                              trigger("time_sync_ntp_servers");
                              trigger("time_sync_ordering");
                            }}
                            LeadingIcon={LuX}
                          />
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
              <FieldError error={formState.errors.time_sync_ntp_servers?.root?.message} />
              <Button
                size="SM"
                theme="light"
                onClick={() => {
                  ntpAppend("", { shouldFocus: true });
                  trigger("time_sync_ntp_servers");
                  trigger("time_sync_ordering");
                }}
                LeadingIcon={LuPlus}
                type="button"
                text={m.network_time_sync_add_ntp_server()}
                disabled={!time_sync_ntp_servers?.every(v => v?.length > 0)}
              />
            </div>
            {/* HTTP server fields */}
            <div className="space-y-4">
              <FieldLabel label={m.network_time_sync_user_http_urls_label()} />
              {httpFields.map((httpServer, httpIndex) => {
                return (
                  <div key={httpServer.id}>
                    <div className="flex items-start gap-x-2">
                      <div className="flex-1">
                        <InputField
                          type="text"
                          size="SM"
                          placeholder="http://www.gstatic.com/generate_204"
                          {...register(`time_sync_http_urls.${httpIndex}`, {
                            validate: (value: string | null) => {
                              if (
                                value == "" ||
                                value === null ||
                                !validator.isURL(value || "", {
                                  protocols: ["http", "https"],
                                  require_tld: false,
                                  require_protocol: true,
                                  require_valid_protocol: true,
                                })
                              ) {
                                return m.network_time_sync_http_url_invalid();
                              }
                              return true;
                            },
                          })}
                          onBlur={() => {
                            trigger("time_sync_http_urls");
                            trigger("time_sync_ordering");
                          }}
                          error={formState.errors.time_sync_http_urls?.[httpIndex]?.message}
                        />
                      </div>
                      {httpIndex >= 0 && (
                        <div className="shrink-0">
                          <Button
                            size="SM"
                            theme="light"
                            type="button"
                            onClick={() => {
                              httpReplace(time_sync_http_urls.filter((_v, i) => i != httpIndex));
                              trigger("time_sync_http_urls");
                              trigger("time_sync_ordering");
                            }}
                            LeadingIcon={LuX}
                          />
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
              <FieldError error={formState.errors.time_sync_http_urls?.root?.message} />
              <Button
                size="SM"
                theme="light"
                onClick={() => {
                  httpAppend("", { shouldFocus: true });
                  trigger("time_sync_http_urls");
                  trigger("time_sync_ordering");
                }}
                LeadingIcon={LuPlus}
                type="button"
                text={m.network_time_sync_add_http_url()}
                disabled={!time_sync_http_urls?.every(v => v?.length > 0)}
              />
            </div>
          </div>
          <SettingsItem
            title={m.network_time_sync_disable_fallback_title()}
            description={m.network_time_sync_disable_fallback_description()}
          >
            <Checkbox
              checked={time_sync_disable_fallback}
              {...register("time_sync_disable_fallback")}
            />
          </SettingsItem>
          <SettingsItem
            title={m.network_time_sync_parallel_title()}
            description={m.network_time_sync_parallel_description()}
          >
            <InputField
              size="SM"
              placeholder={String(time_sync_parallel) || String(4)}
              {...register("time_sync_parallel", {
                valueAsNumber: true,
                validate: (value: string | number) => {
                  return !isNaN(+value) && +value > 0
                    ? true
                    : m.network_time_sync_parallel_invalid();
                },
              })}
              error={formState.errors.time_sync_parallel?.message}
            />
          </SettingsItem>
        </div>
      </div>
    </GridCard>
  );
}
