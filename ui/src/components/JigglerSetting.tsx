import { useCallback } from "react";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "./InputField";

import { useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";

export interface JigglerConfig {
  active_after_seconds: number;
  jitter_enabled: boolean;
  jitter_percentage: number;
  schedule_cron_tab: string;
}

export function JigglerSetting() {
  const [send] = useJsonRpc();
  const [loading, setLoading] = useState(false);

  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>({
    active_after_seconds: 0,
    jitter_enabled: false,
    jitter_percentage: 0.0,
    schedule_cron_tab: "*/20 * * * * *"
  });

  useEffect(() => {
    send("getJigglerConfig", {}, resp => {
      if ("error" in resp) return;
      setJigglerConfigState(resp.result as JigglerConfig);
    });
  }, [send]);

  // const handleJigglerActiveAfterSecondsChange = (value: number) => {
  //   setJigglerConfig({ ...jigglerConfig, active_after_seconds: value });
  // };
  //
  // const handleJigglerJitterEnabledChange = (value: boolean) => {
  //   setJigglerConfig({ ...jigglerConfig, jitter_enabled: value });
  // };
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
        });
      },
      [send],
  );

  return (
    <div className="">
      <div className="grid grid-cols-2 gap-4">
        <InputFieldWithLabel
          required
          label="Jiggler Schedule"
          placeholder="Enter Crontab"
          defaultValue={jigglerConfigState?.schedule_cron_tab}
          onChange={e => handleJigglerScheduleCronTabChange(e.target.value)}
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
