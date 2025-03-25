import { useCallback } from "react";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "./InputField";

import { useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
}

export function JigglerSetting() {
  const [send] = useJsonRpc();
  const [loading, setLoading] = useState(false);

  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>({
    inactivity_limit_seconds: 20.0,
    jitter_percentage:        0.0,
    schedule_cron_tab:        "*/20 * * * * *"
  });

  const syncJigglerConfig = useCallback(() => {
    send("getJigglerConfig", {}, resp => {
      if ("error" in resp) return;
      setJigglerConfigState(resp.result as JigglerConfig);
    });
  }, [send]);

  useEffect(() => {
    syncJigglerConfig()
  }, [send, syncJigglerConfig]);

  const handleJigglerInactivityLimitSecondsChange = (value: string) => {
    setJigglerConfigState({ ...jigglerConfigState, inactivity_limit_seconds: Number(value) });
  };
  //
  // const handleJigglerJitterPercentageChange = (value: number) => {
  //   setJigglerConfig({ ...jigglerConfig, jitter_percentage: value });
  // };

  const handleJigglerScheduleCronTabChange = (value: string) => {
    setJigglerConfigState({ ...jigglerConfigState, schedule_cron_tab: value });
  };

  const handleJigglerConfigChange = useCallback(
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
        <InputFieldWithLabel
          required
          label="Jiggler Schedule"
          placeholder="*/20 * * * * * (Every 20 seconds)"
          value={jigglerConfigState?.schedule_cron_tab}
          onChange={e => handleJigglerScheduleCronTabChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Inactivity Limit (Seconds)"
          placeholder="20"
          value={jigglerConfigState?.inactivity_limit_seconds}
          onChange={e => handleJigglerInactivityLimitSecondsChange(e.target.value)}
        />
      </div>
      <div className="mt-6 flex gap-x-2">
        <Button
          loading={loading}
          size="SM"
          theme="primary"
          text="Update Jiggler Config"
          onClick={() => handleJigglerConfigChange(jigglerConfigState)}
        />
      </div>
    </div>
  );
}
