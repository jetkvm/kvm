import { useCallback, useRef } from "react";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

import { useDiagnosisStore } from "./store";

const POLL_MS = 1000;
const POLLS_PER_PRESET = 8;

interface Preset {
  name: string;
  edid: string;
}

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

export function useEdidSweep() {
  const { send } = useJsonRpc();
  const setSweepTitle = useDiagnosisStore(state => state.setSweepTitle);
  const edidChanged = useDiagnosisStore(state => state.edidChanged);
  const running = useRef(false);

  return useCallback(async () => {
    if (running.current) return;
    running.current = true;

    const call = (method: string, params: unknown) =>
      new Promise<JsonRpcResponse>(resolve => send(method, params, resolve));

    const signalIsGood = async () => {
      let goodPolls = 0;
      for (let poll = 0; poll < POLLS_PER_PRESET; poll++) {
        await sleep(POLL_MS);
        const state = await call("getVideoState", {});
        const ready = !("error" in state) && (state.result as { ready: boolean }).ready;
        goodPolls = ready ? goodPolls + 1 : 0;
        if (goodPolls === 2) return true;
      }
      return false;
    };

    try {
      setSweepTitle(m.diagnosis_sweep_reading());
      const presetsResponse = await call("getEDIDPresets", {});
      const currentResponse = await call("getEDID", {});
      if ("error" in presetsResponse || "error" in currentResponse) {
        notifications.error(m.diagnosis_sweep_read_failed());
        return;
      }
      const presets = presetsResponse.result as Preset[];
      const original = currentResponse.result as string;
      const candidates = presets.filter(
        preset => preset.edid.toLowerCase() !== original.toLowerCase(),
      );

      for (const [index, preset] of candidates.entries()) {
        setSweepTitle(m.diagnosis_sweep_profile({ index: index + 1, count: candidates.length }));
        const response = await call("setEDID", { edid: preset.edid });
        if ("error" in response) continue;
        if (await signalIsGood()) {
          notifications.success(m.diagnosis_sweep_success({ name: preset.name }));
          return;
        }
      }

      setSweepTitle(m.diagnosis_sweep_restoring());
      const originalIsDefault = presets[0]?.edid.toLowerCase() === original.toLowerCase();
      await call("setEDID", { edid: originalIsDefault ? "" : original });
      notifications.error(m.diagnosis_sweep_no_signal());
    } finally {
      setSweepTitle(null);
      edidChanged();
      running.current = false;
    }
  }, [send, setSweepTitle, edidChanged]);
}
