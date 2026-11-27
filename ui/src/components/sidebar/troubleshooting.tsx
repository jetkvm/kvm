import { useEffect, useState } from "react";

import { useUiStore } from "@hooks/stores";
import SidebarHeader from "@components/SidebarHeader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { Button } from "@components/Button";
import { Checks } from "@components/DiagnosisCard";
import { cx } from "@/cva.config";
import { cases, healthy } from "@/diagnosis/cases";
import { WAIT_LIMIT_MS, type Facts } from "@/diagnosis/diagnose";
import { runSelfTest, type SelfTestResult } from "@/diagnosis/selfTest";
import { useDiagnosisStore } from "@/diagnosis/store";

type FactValue = Facts[keyof Facts];

const booleans: FactValue[] = [true, false];
const nullableBooleans: FactValue[] = [true, false, null];

const controls: { key: keyof Facts; values: FactValue[] }[] = [
  { key: "mode", values: ["local", "cloud"] },
  { key: "browser", values: ["chrome", "edge", "firefox", "safari", "other"] },
  { key: "linux", values: booleans },
  { key: "addressIsIp", values: booleans },
  { key: "webrtcSupported", values: booleans },
  { key: "h264Supported", values: booleans },
  { key: "computerOnline", values: booleans },
  { key: "socketOpen", values: booleans },
  { key: "httpReachable", values: nullableBooleans },
  { key: "cloudOnline", values: nullableBooleans },
  { key: "answerState", values: ["none", "accepted", "rejected"] },
  { key: "reachedThroughOtherAddress", values: nullableBooleans },
  { key: "selfTestConnected", values: nullableBooleans },
  { key: "selfTestH264", values: nullableBooleans },
  { key: "iceWasConnected", values: booleans },
  { key: "peerConnected", values: booleans },
  { key: "hdmiState", values: ["ready", "no_signal", "no_lock", "out_of_range", "connecting"] },
  {
    key: "usbState",
    values: ["configured", "suspended", "not attached", "attached", "addressed"],
  },
  { key: "edidIsDefault", values: nullableBooleans },
  { key: "receivingBytes", values: booleans },
  { key: "decodingFrames", values: booleans },
  { key: "codec", values: ["H.264", "H.265", null] },
  { key: "videoPlaying", values: booleans },
  { key: "playError", values: [null, "NotAllowedError", "AbortError"] },
  { key: "msWaiting", values: [0, WAIT_LIMIT_MS] },
];

const LIVE = "live";

const selectClass =
  "h-7 w-full rounded-sm border border-slate-800/30 bg-white py-0 pr-7 pl-2 font-mono text-xs leading-none text-black dark:border-slate-300/20 dark:bg-slate-800 dark:text-white";

export default function TroubleshootingSidebar() {
  const setSidebarView = useUiStore(state => state.setSidebarView);
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);

  useEffect(() => {
    setDisableVideoFocusTrap(true);
    return () => setDisableVideoFocusTrap(false);
  }, [setDisableVideoFocusTrap]);
  const { live, facts, diagnosis, overrides, setOverride, setOverrides } = useDiagnosisStore();
  const overrideCount = Object.keys(overrides).length;
  const [selectedCase, setSelectedCase] = useState(0);
  const [selfTest, setSelfTest] = useState<SelfTestResult | "running" | null>(null);

  const selectCase = (index: number) => {
    setSelectedCase(index);
    setOverrides(index === 0 ? {} : { ...healthy, ...cases[index].facts });
  };

  return (
    <div
      className="grid h-full grid-rows-(--grid-headerBody) shadow-xs"
      onKeyDown={event => event.stopPropagation()}
      onKeyUp={event => event.stopPropagation()}
    >
      <SidebarHeader title="Troubleshooting" setSidebarView={setSidebarView} />
      <div className="h-full space-y-8 overflow-y-scroll bg-white px-4 py-2 pb-8 text-sm text-black dark:bg-slate-900 dark:text-white">
        <div className="space-y-3">
          <SettingsSectionHeader
            title="Result"
            description="What the page shows for the current facts."
          />
          <div className="space-y-1">
            <div className="font-semibold">
              {diagnosis.status === "ok" ? "No problem found" : diagnosis.title}
            </div>
            <div className="font-mono text-xs text-slate-600 dark:text-slate-400">
              status={diagnosis.status}
              {diagnosis.status !== "ok" && ` question=${diagnosis.question}`}
              {diagnosis.status === "failed" && ` level=${diagnosis.level}`}
              {facts && ` waited=${(facts.msWaiting / 1000).toFixed(1)}s`}
            </div>
          </div>
          <Checks diagnosis={diagnosis} />
        </div>

        <div className="space-y-3">
          <SettingsSectionHeader
            title="Simulate a case"
            description="Sets the facts of a documented failure. The real session is not changed."
          />
          <div
            role="radiogroup"
            aria-label="Case"
            className="max-h-80 overflow-y-auto rounded-sm border border-slate-800/30 p-1 dark:border-slate-300/20"
          >
            {cases.map((c, index) => (
              <label
                key={c.name}
                className="flex cursor-pointer items-center gap-x-2 rounded-sm px-2 py-1 text-xs hover:bg-blue-50 has-checked:bg-blue-100 has-checked:font-semibold dark:hover:bg-slate-800 dark:has-checked:bg-slate-700"
              >
                <input
                  type="radio"
                  name="troubleshooting-case"
                  className="h-3 w-3"
                  checked={index === selectedCase}
                  onChange={() => selectCase(index)}
                />
                {index === 0 ? "Live session" : c.name}
              </label>
            ))}
          </div>
          <Button
            size="XS"
            theme="light"
            text={`Clear ${overrideCount} override${overrideCount === 1 ? "" : "s"}`}
            disabled={overrideCount === 0}
            onClick={() => selectCase(0)}
          />
        </div>

        <div className="space-y-3">
          <SettingsSectionHeader
            title="Self-test"
            description="A WebRTC connection inside this browser, with an H.264 test video."
          />
          <Button
            size="XS"
            theme="light"
            text="Run the self-test"
            disabled={selfTest === "running"}
            onClick={() => {
              setSelfTest("running");
              void runSelfTest().then(setSelfTest);
            }}
          />
          {selfTest && (
            <div className="font-mono text-xs text-slate-600 dark:text-slate-400">
              {selfTest === "running"
                ? "running"
                : `connected=${selfTest.connected} h264=${selfTest.h264}`}
            </div>
          )}
        </div>

        <div className="space-y-3">
          <SettingsSectionHeader
            title="Facts"
            description="The live value of each fact, and a control to override it."
          />
          <div className="grid grid-cols-[1fr_auto_8.5rem] items-center gap-x-3 gap-y-1.5">
            <div className="text-xs font-semibold text-slate-500 dark:text-slate-400">Fact</div>
            <div className="text-xs font-semibold text-slate-500 dark:text-slate-400">Live</div>
            <div className="text-xs font-semibold text-slate-500 dark:text-slate-400">Override</div>
            {controls.map(({ key, values }) => {
              const overridden = key in overrides;
              return (
                <div key={key} className="contents">
                  <div className={cx("font-mono text-xs", overridden && "font-semibold")}>
                    {key}
                  </div>
                  <div
                    className={cx(
                      "text-right font-mono text-xs",
                      overridden
                        ? "text-slate-400 line-through dark:text-slate-500"
                        : "text-slate-700 dark:text-slate-300",
                    )}
                  >
                    {live ? String(live[key]) : "-"}
                  </div>
                  <select
                    className={cx(
                      selectClass,
                      overridden && "border-blue-700 dark:border-blue-500",
                    )}
                    value={overridden ? String(values.indexOf(overrides[key] as FactValue)) : LIVE}
                    onChange={event => {
                      const value =
                        event.target.value === LIVE
                          ? undefined
                          : values[Number(event.target.value)];
                      setOverride(key, value as never);
                    }}
                  >
                    <option value={LIVE}>live</option>
                    {values.map((value, index) => (
                      <option key={String(value)} value={index}>
                        {String(value)}
                      </option>
                    ))}
                  </select>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
