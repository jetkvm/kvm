import { useCallback } from "react";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "./InputField";

import { useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
}

const jigglerCrontabConfigs = [
  {
    label: "Every 20 seconds",
    value: "*/20 * * * * *",
  },
  {
    label: "Every 40 seconds",
    value: "*/40 * * * * *",
  },
  {
    label: "Every 1 minute",
    value: "0 * * * * *",
  },
  {
    label: "Every 3 minutes",
    value: "0 */3 * * * *",
  },
];

const jigglerJitterConfigs = [
  {
    label: "No Jitter",
    value: "0.0",
  },
  {
    label: "10%",
    value: ".1",
  },
  {
    label: "25%",
    value: ".25",
  },
  {
    label: "50%",
    value: ".5",
  },
];

const jigglerInactivityConfigs = [
  {
    label: "20 Seconds",
    value: "20",
  },
  {
    label: "40 Seconds",
    value: "40",
  },
  {
    label: "1 Minute",
    value: "60",
  },
  {
    label: "3 Minutes",
    value: "180",
  },
];

export function JigglerSetting() {
  const [send] = useJsonRpc();
  const [loading, setLoading] = useState(false);
  const [inactivityLimitSeconds, setInactivityLimitSeconds] = useState("");
  const [jitterPercentage, setJitterPercentage] = useState("");
  const [scheduleCronTab, setScheduleCronTab] = useState("");

  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>({
    inactivity_limit_seconds: 20.0,
    jitter_percentage:        0.0,
    schedule_cron_tab:        "*/20 * * * * *"
  });

  const syncJigglerConfig = useCallback(() => {
    send("getJigglerConfig", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as JigglerConfig;
      setJigglerConfigState(result);
      setInactivityLimitSeconds(String(result.inactivity_limit_seconds))
      setJitterPercentage(String(result.jitter_percentage))
      setScheduleCronTab(result.schedule_cron_tab)
    });
  }, [send, setInactivityLimitSeconds]);

  useEffect(() => {
    syncJigglerConfig()
  }, [send, syncJigglerConfig]);

  const handleJigglerInactivityLimitSecondsChange = (value: string) => {
    setInactivityLimitSeconds(value)
    setJigglerConfigState({ ...jigglerConfigState, inactivity_limit_seconds: Number(value) });
  };

  const handleJigglerJitterPercentageChange = (value: string) => {
    setJitterPercentage(value)
    setJigglerConfigState({ ...jigglerConfigState, jitter_percentage: Number(value) });
  };

  const handleJigglerScheduleCronTabChange = (value: string) => {
    setScheduleCronTab(value)
    setJigglerConfigState({ ...jigglerConfigState, schedule_cron_tab: value });
  };

  const handleJigglerConfigSave = useCallback(
    (jigglerConfig: JigglerConfig) => {
      setLoading(true);
      send("setJigglerConfig", { jigglerConfig }, async resp => {
        if ("error" in resp) {
          notifications.error(
              `Failed to set jiggler config: ${resp.error.data || "Unknown error"}`,
          );
          setLoading(false);
          return;
        }
        setLoading(false);
        notifications.success(
          `Jiggler Config successfully updated`,
        );
        syncJigglerConfig();
      });
    },
    [send, syncJigglerConfig],
  );

  return (
    <div className="">
      <div className="grid grid-cols-2 gap-4">
        <SelectMenuBasic
          size="SM"
          label="Schedule"
          className="max-w-[192px]"
          value={scheduleCronTab}
          fullWidth
          onChange={e => {
            if (e.target.value === "custom") {
              setScheduleCronTab(e.target.value);
            } else {
              handleJigglerScheduleCronTabChange(e.target.value)
            }
          }}
          options={[...jigglerCrontabConfigs, { value: "custom", label: "Custom" }]}
        />
        {jitterPercentage === "custom" && (
          <InputFieldWithLabel
            required
            label="Jiggler Crontab"
            placeholder="Enter Cron Tab"
            value={scheduleCronTab}
            onChange={e => handleJigglerScheduleCronTabChange(e.target.value)}
          />
        )}
        <SelectMenuBasic
          size="SM"
          label="Jitter Percentage"
          className="max-w-[192px]"
          value={jitterPercentage}
          fullWidth
          onChange={e => {
            if (e.target.value === "custom") {
              setJitterPercentage(e.target.value);
            } else {
              handleJigglerJitterPercentageChange(e.target.value)
            }
          }}
          options={[...jigglerJitterConfigs, { value: "custom", label: "Custom" }]}
        />
        {jitterPercentage === "custom" && (
          <InputFieldWithLabel
            required
            label="Jitter Percentage"
            placeholder="0.0"
            onChange={e => handleJigglerJitterPercentageChange(e.target.value)}
          />
        )}
        <SelectMenuBasic
          size="SM"
          label="Inactivity Limit Seconds"
          className="max-w-[192px]"
          value={inactivityLimitSeconds}
          fullWidth
          onChange={e => {
            handleJigglerInactivityLimitSecondsChange(e.target.value);
          }}
          options={[...jigglerInactivityConfigs]}
        />
      </div>
      <div className="mt-6 flex gap-x-2">
        <Button
          loading={loading}
          size="SM"
          theme="primary"
          text="Update Jiggler Config"
          onClick={() => handleJigglerConfigSave(jigglerConfigState)}
        />
      </div>
    </div>
  );
}
