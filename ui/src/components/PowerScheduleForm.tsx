import { useEffect, useMemo, useState } from "react";

import { cx } from "@/cva.config";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { m } from "@localizations/messages.js";

export type PowerScheduleMethod = "wol" | "atx";
export type PowerScheduleAction = "on" | "off" | "off-force";

export interface PowerSchedule {
  id: string;
  name: string;
  enabled: boolean;
  method: PowerScheduleMethod;
  action: PowerScheduleAction;
  /** 0 = Sunday .. 6 = Saturday, matching the Go backend. */
  weekdays: number[];
  hour: number;
  minute: number;
  timezone: string;
  macAddress?: string;
  broadcastIP?: string;
}

export interface WakeOnLanDevice {
  name: string;
  macAddress: string;
  broadcastIP?: string;
}

export const MAX_POWER_SCHEDULES = 25;

const MAC_PATTERN = "^([0-9a-fA-F][0-9a-fA-F]:){5}([0-9a-fA-F][0-9a-fA-F])$";
const MAC_REGEX = new RegExp(MAC_PATTERN);

/** Weekday values in Monday-first display order. */
export const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0];
export const WEEKDAY_PRESETS = {
  weekdays: [1, 2, 3, 4, 5],
  weekend: [0, 6],
  daily: [0, 1, 2, 3, 4, 5, 6],
};

export function generateScheduleId() {
  return Math.random().toString(36).substring(2, 9);
}

export function browserTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

export function actionsForMethod(method: PowerScheduleMethod): PowerScheduleAction[] {
  // A magic packet can only ever turn a host on.
  return method === "wol" ? ["on"] : ["on", "off", "off-force"];
}

export function actionLabel(action: PowerScheduleAction) {
  switch (action) {
    case "on":
      return m.scheduler_action_on();
    case "off":
      return m.scheduler_action_off();
    case "off-force":
      return m.scheduler_action_off_force();
  }
}

export function methodLabel(method: PowerScheduleMethod) {
  return method === "wol" ? m.scheduler_method_wol() : m.scheduler_method_atx();
}

export function shortWeekdayName(weekday: number) {
  // 2024-01-07 was a Sunday, so adding the weekday index lands on the right day.
  const date = new Date(Date.UTC(2024, 0, 7 + weekday));
  return new Intl.DateTimeFormat(undefined, { weekday: "short", timeZone: "UTC" }).format(date);
}

export function formatTime(hour: number, minute: number) {
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

/** Renders the weekday set as "Every day", "Mon–Fri", "Sat, Sun" etc. */
export function formatWeekdays(weekdays: number[]) {
  const sorted = [...weekdays].sort((a, b) => a - b);
  const key = sorted.join(",");
  if (key === WEEKDAY_PRESETS.daily.join(",")) return m.scheduler_preset_daily();
  if (key === WEEKDAY_PRESETS.weekdays.join(",")) return m.scheduler_preset_weekdays();
  if (key === [...WEEKDAY_PRESETS.weekend].sort((a, b) => a - b).join(","))
    return m.scheduler_preset_weekend();
  return WEEKDAY_ORDER.filter(d => weekdays.includes(d))
    .map(shortWeekdayName)
    .join(", ");
}

/**
 * Describes the next firing as a human label ("Today at 08:00", "Mon at 08:00").
 *
 * We deliberately compare wall-clock fields inside the schedule's own timezone
 * instead of converting to absolute time: it sidesteps DST edge cases and the
 * label only ever needs a day-relative description anyway.
 */
export function describeNextRun(schedule: PowerSchedule): string | null {
  if (schedule.weekdays.length === 0) return null;

  let parts: Intl.DateTimeFormatPart[];
  try {
    parts = new Intl.DateTimeFormat("en-US", {
      timeZone: schedule.timezone || "UTC",
      weekday: "short",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).formatToParts(new Date());
  } catch {
    return null;
  }

  const lookup = (type: string) => parts.find(p => p.type === type)?.value ?? "";
  const weekdayNames = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const nowWeekday = weekdayNames.indexOf(lookup("weekday"));
  if (nowWeekday < 0) return null;

  // "24" shows up at midnight in some hour12:false implementations.
  const nowHour = Number(lookup("hour")) % 24;
  const nowMinute = Number(lookup("minute"));

  const nowMinutes = nowHour * 60 + nowMinute;
  const targetMinutes = schedule.hour * 60 + schedule.minute;
  const time = formatTime(schedule.hour, schedule.minute);

  for (let offset = 0; offset <= 7; offset++) {
    const weekday = (nowWeekday + offset) % 7;
    if (!schedule.weekdays.includes(weekday)) continue;
    if (offset === 0 && targetMinutes <= nowMinutes) continue;
    if (offset === 0) return m.scheduler_next_run_today({ time });
    if (offset === 1) return m.scheduler_next_run_tomorrow({ time });
    return m.scheduler_next_run_weekday({ weekday: shortWeekdayName(weekday), time });
  }
  return null;
}

export function emptySchedule(timezone: string): PowerSchedule {
  return {
    id: generateScheduleId(),
    name: "",
    enabled: true,
    method: "wol",
    action: "on",
    weekdays: [...WEEKDAY_PRESETS.weekdays],
    hour: 8,
    minute: 0,
    timezone,
    macAddress: "",
  };
}

interface PowerScheduleFormProps {
  schedule: PowerSchedule;
  timezones: string[];
  wolDevices: WakeOnLanDevice[];
  atxAvailable: boolean;
  isSaving: boolean;
  onSave: (schedule: PowerSchedule) => void;
  onCancel: () => void;
}

export default function PowerScheduleForm({
  schedule: initialSchedule,
  timezones,
  wolDevices,
  atxAvailable,
  isSaving,
  onSave,
  onCancel,
}: PowerScheduleFormProps) {
  // A new schedule starts on the first saved Wake-on-LAN device when there is
  // one, so the select and the schedule never disagree about what's chosen.
  const [schedule, setSchedule] = useState<PowerSchedule>(() =>
    !initialSchedule.macAddress && wolDevices.length > 0
      ? {
          ...initialSchedule,
          macAddress: wolDevices[0].macAddress,
          broadcastIP: wolDevices[0].broadcastIP,
        }
      : initialSchedule,
  );
  const [macMode, setMacMode] = useState<string>(() => {
    if (!initialSchedule.macAddress) {
      return wolDevices.length > 0 ? wolDevices[0].macAddress : "custom";
    }
    return wolDevices.some(d => d.macAddress === initialSchedule.macAddress)
      ? initialSchedule.macAddress
      : "custom";
  });

  const update = (patch: Partial<PowerSchedule>) => setSchedule(prev => ({ ...prev, ...patch }));

  // Keep the action legal whenever the method changes.
  useEffect(() => {
    const allowed = actionsForMethod(schedule.method);
    if (!allowed.includes(schedule.action)) {
      setSchedule(prev => ({ ...prev, action: allowed[0] }));
    }
  }, [schedule.method, schedule.action]);

  const timezoneOptions = useMemo(
    () => timezones.map(tz => ({ value: tz, label: tz })),
    [timezones],
  );

  const macOptions = useMemo(
    () => [
      ...wolDevices.map(d => ({ value: d.macAddress, label: `${d.name} (${d.macAddress})` })),
      { value: "custom", label: m.scheduler_device_custom() },
    ],
    [wolDevices],
  );

  const toggleWeekday = (weekday: number) => {
    setSchedule(prev => ({
      ...prev,
      weekdays: prev.weekdays.includes(weekday)
        ? prev.weekdays.filter(d => d !== weekday)
        : [...prev.weekdays, weekday].sort((a, b) => a - b),
    }));
  };

  const macIsValid = schedule.method !== "wol" || MAC_REGEX.test(schedule.macAddress ?? "");
  const canSave =
    schedule.name.trim().length > 0 && schedule.weekdays.length > 0 && macIsValid && !isSaving;

  const handleMacModeChange = (value: string) => {
    setMacMode(value);
    if (value === "custom") {
      update({ macAddress: "", broadcastIP: undefined });
      return;
    }
    const device = wolDevices.find(d => d.macAddress === value);
    update({ macAddress: value, broadcastIP: device?.broadcastIP });
  };

  return (
    <div className="space-y-4 rounded-md border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-800">
      <h3 className="text-sm font-semibold text-black dark:text-white">
        {m.scheduler_form_title()}
      </h3>

      <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
        <InputFieldWithLabel
          required
          size="SM"
          label={m.scheduler_name_label()}
          placeholder={m.scheduler_name_placeholder()}
          value={schedule.name}
          maxLength={50}
          onKeyUp={e => e.stopPropagation()}
          onChange={e => update({ name: e.target.value })}
        />

        <SelectMenuBasic
          size="SM"
          label={m.scheduler_method_label()}
          description={atxAvailable ? undefined : m.scheduler_method_atx_unavailable()}
          value={schedule.method}
          onChange={e => update({ method: e.target.value as PowerScheduleMethod })}
          options={[
            { value: "wol", label: m.scheduler_method_wol() },
            { value: "atx", label: m.scheduler_method_atx(), disabled: !atxAvailable },
          ]}
        />

        <SelectMenuBasic
          size="SM"
          label={m.scheduler_action_label()}
          description={
            schedule.method === "wol"
              ? m.scheduler_action_wol_description()
              : m.scheduler_action_atx_description()
          }
          value={schedule.action}
          disabled={actionsForMethod(schedule.method).length === 1}
          onChange={e => update({ action: e.target.value as PowerScheduleAction })}
          options={actionsForMethod(schedule.method).map(a => ({
            value: a,
            label: actionLabel(a),
          }))}
        />

        {schedule.method === "wol" && (
          <SelectMenuBasic
            size="SM"
            label={m.scheduler_device_label()}
            description={m.scheduler_device_description()}
            value={macMode}
            onChange={e => handleMacModeChange(e.target.value)}
            options={macOptions}
          />
        )}

        {schedule.method === "wol" && macMode === "custom" && (
          <InputFieldWithLabel
            required
            size="SM"
            label={m.scheduler_mac_label()}
            placeholder="00:b0:d0:63:c2:26"
            pattern={MAC_PATTERN}
            minLength={17}
            maxLength={17}
            value={schedule.macAddress ?? ""}
            onKeyUp={e => e.stopPropagation()}
            onChange={e => update({ macAddress: e.target.value })}
          />
        )}

        <InputFieldWithLabel
          required
          size="SM"
          type="time"
          label={m.scheduler_time_label()}
          value={formatTime(schedule.hour, schedule.minute)}
          onKeyUp={e => e.stopPropagation()}
          onChange={e => {
            const [hour, minute] = e.target.value.split(":").map(Number);
            if (Number.isFinite(hour) && Number.isFinite(minute)) update({ hour, minute });
          }}
        />

        <SelectMenuBasic
          size="SM"
          label={m.scheduler_timezone_label()}
          description={m.scheduler_timezone_description()}
          value={schedule.timezone}
          disabled={timezones.length === 0}
          onChange={e => update({ timezone: e.target.value })}
          options={timezoneOptions}
        />
      </div>

      <div className="space-y-2">
        <div className="text-[13px] font-medium text-black dark:text-white">
          {m.scheduler_days_label()}
        </div>
        <div className="flex flex-wrap gap-1.5">
          {WEEKDAY_ORDER.map(weekday => {
            const active = schedule.weekdays.includes(weekday);
            return (
              <button
                key={weekday}
                type="button"
                aria-pressed={active}
                onClick={() => toggleWeekday(weekday)}
                className={cx(
                  "min-w-12 rounded-md border px-2.5 py-1.5 text-xs font-medium transition-colors",
                  active
                    ? "border-blue-700 bg-blue-700 text-white dark:border-blue-500 dark:bg-blue-500"
                    : "border-slate-300 bg-white text-slate-700 hover:bg-slate-100 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-700",
                )}
              >
                {shortWeekdayName(weekday)}
              </button>
            );
          })}
        </div>
        <div className="flex flex-wrap gap-2 pt-1">
          <Button
            size="XS"
            theme="light"
            text={m.scheduler_preset_weekdays()}
            onClick={() => update({ weekdays: [...WEEKDAY_PRESETS.weekdays] })}
          />
          <Button
            size="XS"
            theme="light"
            text={m.scheduler_preset_weekend()}
            onClick={() => update({ weekdays: [...WEEKDAY_PRESETS.weekend].sort((a, b) => a - b) })}
          />
          <Button
            size="XS"
            theme="light"
            text={m.scheduler_preset_daily()}
            onClick={() => update({ weekdays: [...WEEKDAY_PRESETS.daily] })}
          />
        </div>
      </div>

      <div className="flex gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text={m.scheduler_save()}
          disabled={!canSave}
          onClick={() =>
            onSave({
              ...schedule,
              name: schedule.name.trim(),
              // Don't persist Wake-on-LAN fields on an ATX schedule.
              ...(schedule.method === "atx"
                ? { macAddress: undefined, broadcastIP: undefined }
                : {}),
            })
          }
        />
        <Button size="SM" theme="light" text={m.cancel()} onClick={onCancel} />
      </div>
    </div>
  );
}
