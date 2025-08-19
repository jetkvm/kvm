import { useEffect, useMemo, useState } from "react";
import { LuExternalLink } from "react-icons/lu";

import { Button, LinkButton } from "@components/Button";
import { useJsonRpc } from "@/hooks/useJsonRpc";

import { InputFieldWithLabel } from "./InputField";
import { SelectMenuBasic } from "./SelectMenuBasic";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
  timezone?: string;
}

export function JigglerSetting({
  onSave,
  defaultJigglerState,
}: {
  onSave: (jigglerConfig: JigglerConfig) => void;
  defaultJigglerState?: JigglerConfig;
}) {
  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>(
    defaultJigglerState || {
      inactivity_limit_seconds: 20,
      jitter_percentage: 0,
      schedule_cron_tab: "*/20 * * * * *",
      timezone: "UTC",
    },
  );

  const [send] = useJsonRpc();
  const [timezones, setTimezones] = useState<string[]>([]);

  useEffect(() => {
    send("getTimezones", {}, resp => {
      if ("error" in resp) return;
      setTimezones(resp.result as string[]);
    });
  }, [send]);

  const timezoneOptions = useMemo(
    () =>
      timezones.map((timezone: string) => ({
        value: timezone,
        label: timezone,
      })),
    [timezones],
  );

  const exampleConfigs = [
    {
      name: "Business Hours 9-17",
      config: {
        inactivity_limit_seconds: 60,
        jitter_percentage: 25,
        schedule_cron_tab: "0 * 9-17 * * 1-5",
        timezone: jigglerConfigState.timezone || "UTC",
      },
    },
    {
      name: "Business Hours 8-17",
      config: {
        inactivity_limit_seconds: 60,
        jitter_percentage: 25,
        schedule_cron_tab: "0 * 8-17 * * 1-5",
        timezone: jigglerConfigState.timezone || "UTC",
      },
    },
  ];

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <h4 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
          Examples
        </h4>
        <div className="flex flex-wrap gap-2">
          {exampleConfigs.map((example, index) => (
            <Button
              key={index}
              size="XS"
              theme="light"
              text={example.name}
              onClick={() => setJigglerConfigState(example.config)}
            />
          ))}
          <LinkButton
            to="https://crontab.guru/examples.html"
            size="XS"
            theme="light"
            text="More examples"
            LeadingIcon={LuExternalLink}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 items-end gap-4 md:grid-cols-2">
        <InputFieldWithLabel
          required
          size="SM"
          label="Cron Schedule"
          description="Cron expression for scheduling"
          placeholder="*/20 * * * * *"
          value={jigglerConfigState.schedule_cron_tab}
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              schedule_cron_tab: e.target.value,
            })
          }
        />

        <InputFieldWithLabel
          size="SM"
          label="Inactivity Limit Seconds"
          description="Inactivity time before jiggle"
          value={jigglerConfigState.inactivity_limit_seconds}
          type="number"
          min="1"
          max="100"
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              inactivity_limit_seconds: Number(e.target.value),
            })
          }
        />

        <InputFieldWithLabel
          required
          size="SM"
          label="Random delay"
          description="To avoid recognizable patterns"
          placeholder="25"
          TrailingElm={<span className="px-2 text-xs text-slate-500">%</span>}
          value={jigglerConfigState.jitter_percentage}
          type="number"
          min="0"
          max="100"
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              jitter_percentage: Number(e.target.value),
            })
          }
        />

        <SelectMenuBasic
          size="SM"
          label="Timezone"
          description="Timezone for cron schedule"
          value={jigglerConfigState.timezone || "UTC"}
          disabled={timezones.length === 0}
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              timezone: e.target.value,
            })
          }
          options={timezoneOptions}
        />
      </div>

      <div className="flex gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Save Jiggler Config"
          onClick={() => onSave(jigglerConfigState)}
        />
      </div>
    </div>
  );
}
