import { useState } from "react";

import { Button } from "@components/Button";

import { InputFieldWithLabel } from "./InputField";
import ExtLink from "./ExtLink";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
}

export function JigglerSetting({
  onSave,
}: {
  onSave: (jigglerConfig: JigglerConfig) => void;
}) {
  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>({
    inactivity_limit_seconds: 20,
    jitter_percentage: 0,
    schedule_cron_tab: "*/20 * * * * *",
  });

  return (
    <div className="space-y-2">
      <div className="grid max-w-sm grid-cols-1 items-end gap-y-2">
        <InputFieldWithLabel
          required
          size="SM"
          label="Cron Schedule"
          description={
            <span>
              Generate with{" "}
              <ExtLink className="text-blue-700 underline" href="https://crontab.guru/">
                crontab.guru
              </ExtLink>
            </span>
          }
          placeholder="*/20 * * * * *"
          defaultValue={jigglerConfigState.schedule_cron_tab}
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
          description="Seconds of inactivity before triggering a jiggle again"
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
          defaultValue={jigglerConfigState.jitter_percentage}
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
      </div>

      <div className="mt-6 flex gap-x-2">
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
