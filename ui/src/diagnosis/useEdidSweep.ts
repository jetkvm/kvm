import { useCallback, useRef } from "react";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

import { useDiagnosisStore } from "./store";

const POLL_MS = 1000;
const POLLS_PER_PRESET = 8;
const RPC_TIMEOUT_MS = 15_000;

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
      new Promise<JsonRpcResponse>((resolve, reject) => {
        const timeout = setTimeout(
          () => reject(new Error(`${method} got no reply`)),
          RPC_TIMEOUT_MS,
        );
        void send(method, params, response => {
          clearTimeout(timeout);
          resolve(response);
        });
      });

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

    let restoreEdid: string | null = null;
    const restorePrevious = async () => {
      if (restoreEdid === null) return;
      setSweepTitle(m.diagnosis_sweep_restoring());
      const restored = await call("setEDID", { edid: restoreEdid }).catch(() => null);
      notifications.error(
        !restored || "error" in restored
          ? m.diagnosis_sweep_restore_failed()
          : m.diagnosis_sweep_no_signal(),
      );
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
      const originalIsDefault = presets[0]?.edid.toLowerCase() === original.toLowerCase();
      const candidates = presets.filter(
        preset => preset.edid.toLowerCase() !== original.toLowerCase(),
      );

      for (const [index, preset] of candidates.entries()) {
        setSweepTitle(m.diagnosis_sweep_profile({ index: index + 1, count: candidates.length }));
        restoreEdid = originalIsDefault ? "" : original;
        const response = await call("setEDID", { edid: preset.edid });
        if ("error" in response) continue;
        if (await signalIsGood()) {
          notifications.success(m.diagnosis_sweep_success({ name: preset.name }));
          return;
        }
      }

      await restorePrevious();
    } catch (error) {
      console.error("The display profile sweep stopped", error);
      if (restoreEdid === null) notifications.error(m.diagnosis_sweep_read_failed());
      else await restorePrevious();
    } finally {
      setSweepTitle(null);
      edidChanged();
      running.current = false;
    }
  }, [send, setSweepTitle, edidChanged]);
}
