import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import { SelectMenuBasic } from "@components/SelectMenuBasic";

import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";

import { InputFieldWithLabel } from "./InputField";


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
    value: "0",
  },
  {
    label: "10%",
    value: "20",
  },
  {
    label: "25%",
    value: "25",
  },
  {
    label: "50%",
    value: "50",
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
  const [jitterPercentage, setJitterPercentage] = useState("");
  const [scheduleCronTab, setScheduleCronTab] = useState("");

  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>({
    inactivity_limit_seconds: 20,
    jitter_percentage:        0,
    schedule_cron_tab:        "*/20 * * * * *"
  });

  const syncJigglerConfig = useCallback(() => {
    send("getJigglerConfig", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as JigglerConfig;
      setJigglerConfigState(result);

      const jitterPercentage = jigglerJitterConfigs.map(u => u.value).includes(result.jitter_percentage.toString())
        ? result.jitter_percentage.toString()
        : "custom";
      setJitterPercentage(jitterPercentage)

      const scheduleCronTab = jigglerCrontabConfigs.map(u => u.value).includes(result.schedule_cron_tab)
        ? result.schedule_cron_tab
        : "custom";
      setScheduleCronTab(scheduleCronTab)
    });
  }, [send]);

  useEffect(() => {
    syncJigglerConfig()
  }, [send, syncJigglerConfig]);

  const handleJigglerInactivityLimitSecondsChange = (value: string) => {
    setJigglerConfigState({ ...jigglerConfigState, inactivity_limit_seconds: Number(value) });
  };

  const handleJigglerJitterPercentageChange = (value: string) => {
    setJigglerConfigState({ ...jigglerConfigState, jitter_percentage: Number(value) });
  };

  const handleJigglerScheduleCronTabChange = (value: string) => {
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
            setScheduleCronTab(e.target.value);
            if (e.target.value != "custom") {
              handleJigglerScheduleCronTabChange(e.target.value);
            }
          }}
          options={[...jigglerCrontabConfigs, {value: "custom", label: "Custom"}]}
        />
        {scheduleCronTab === "custom" && (
          <InputFieldWithLabel
            required
            label="Jiggler Crontab"
            placeholder="*/20 * * * * *"
            onChange={e => handleJigglerScheduleCronTabChange(e.target.value)}
          />
        )}
      </div>
      <div className="grid grid-cols-2 gap-4">
        <SelectMenuBasic
          size="SM"
          label="Jitter Percentage"
          className="max-w-[192px]"
          value={jitterPercentage}
          fullWidth
          onChange={e => {
            setJitterPercentage(e.target.value);
            if (e.target.value != "custom") {
              handleJigglerJitterPercentageChange(e.target.value)
            }
          }}
          options={[...jigglerJitterConfigs, {value: "custom", label: "Custom"}]}
        />
        {jitterPercentage === "custom" && (
          <InputFieldWithLabel
            required
            label="Jitter Percentage"
            placeholder="30"
            type="number"
            min="1"
            max="100"
            onChange={e => handleJigglerJitterPercentageChange(e.target.value)}
          />
        )}
      </div>
      <div className="grid grid-cols-2 gap-4">
        <SelectMenuBasic
          size="SM"
          label="Inactivity Limit Seconds"
          className="max-w-[192px]"
          value={jigglerConfigState.inactivity_limit_seconds}
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
