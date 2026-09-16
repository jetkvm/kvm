import { useCallback, useEffect, useMemo, useState } from "react";
import { LuCalendarClock, LuPenLine, LuTrash2 } from "react-icons/lu";

import { cx } from "@/cva.config";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { Checkbox } from "@components/Checkbox";
import { ConfirmDialog } from "@components/ConfirmDialog";
import EmptyCard from "@components/EmptyCard";
import LoadingSpinner from "@components/LoadingSpinner";
import PowerScheduleForm, {
  MAX_POWER_SCHEDULES,
  PowerSchedule,
  WakeOnLanDevice,
  actionLabel,
  browserTimezone,
  describeNextRun,
  emptySchedule,
  formatTime,
  formatWeekdays,
  methodLabel,
} from "@components/PowerScheduleForm";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export default function SettingsSchedulerRoute() {
  const { send } = useJsonRpc();

  const [schedules, setSchedules] = useState<PowerSchedule[]>([]);
  const [timezones, setTimezones] = useState<string[]>([]);
  const [wolDevices, setWolDevices] = useState<WakeOnLanDevice[]>([]);
  const [activeExtension, setActiveExtension] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [editing, setEditing] = useState<PowerSchedule | null>(null);
  const [scheduleToDelete, setScheduleToDelete] = useState<PowerSchedule | null>(null);

  const atxAvailable = activeExtension === "atx-power";
  const isMaxReached = schedules.length >= MAX_POWER_SCHEDULES;

  useEffect(() => {
    let settled = 0;
    const settle = () => {
      settled++;
      if (settled >= 4) setLoading(false);
    };

    send("getPowerSchedules", {}, (resp: JsonRpcResponse) => {
      if ("result" in resp) setSchedules((resp.result as PowerSchedule[]) ?? []);
      settle();
    });
    send("getTimezones", {}, (resp: JsonRpcResponse) => {
      if ("result" in resp) setTimezones(resp.result as string[]);
      settle();
    });
    send("getWakeOnLanDevices", {}, (resp: JsonRpcResponse) => {
      if ("result" in resp) setWolDevices((resp.result as WakeOnLanDevice[]) ?? []);
      settle();
    });
    send("getActiveExtension", {}, (resp: JsonRpcResponse) => {
      if ("result" in resp) setActiveExtension(resp.result as string);
      settle();
    });
  }, [send]);

  /**
   * The backend owns the whole list, so every mutation sends the full array.
   * We only update local state once the device has accepted it.
   */
  const persist = useCallback(
    (next: PowerSchedule[], successMessage: string) => {
      setIsSaving(true);
      send("setPowerSchedules", { params: { schedules: next } }, (resp: JsonRpcResponse) => {
        setIsSaving(false);
        if ("error" in resp) {
          notifications.error(
            m.scheduler_failed_save({
              error: resp.error.data || resp.error.message || m.unknown_error(),
            }),
          );
          return;
        }
        setSchedules(next);
        setEditing(null);
        setScheduleToDelete(null);
        notifications.success(successMessage);
      });
    },
    [send],
  );

  const handleSave = useCallback(
    (schedule: PowerSchedule) => {
      const exists = schedules.some(s => s.id === schedule.id);
      const next = exists
        ? schedules.map(s => (s.id === schedule.id ? schedule : s))
        : [...schedules, schedule];
      persist(
        next,
        exists
          ? m.scheduler_updated_success({ name: schedule.name })
          : m.scheduler_created_success({ name: schedule.name }),
      );
    },
    [schedules, persist],
  );

  const handleToggleEnabled = useCallback(
    (schedule: PowerSchedule) => {
      const next = schedules.map(s => (s.id === schedule.id ? { ...s, enabled: !s.enabled } : s));
      persist(
        next,
        schedule.enabled
          ? m.scheduler_disabled_success({ name: schedule.name })
          : m.scheduler_enabled_success({ name: schedule.name }),
      );
    },
    [schedules, persist],
  );

  const handleDelete = useCallback(() => {
    if (!scheduleToDelete) return;
    persist(
      schedules.filter(s => s.id !== scheduleToDelete.id),
      m.scheduler_deleted_success({ name: scheduleToDelete.name }),
    );
  }, [scheduleToDelete, schedules, persist]);

  const defaultTimezone = useMemo(() => {
    const tz = browserTimezone();
    return timezones.length === 0 || timezones.includes(tz) ? tz : "UTC";
  }, [timezones]);

  const scheduleList = (
    <div className="space-y-2">
      {schedules.map(schedule => {
        const nextRun = schedule.enabled ? describeNextRun(schedule) : null;
        // An ATX schedule is inert while the extension is switched off.
        const stale = schedule.method === "atx" && !atxAvailable;

        return (
          <Card key={schedule.id} className="bg-white p-3 dark:bg-slate-800">
            <div className="flex items-center justify-between gap-x-4">
              <div className="flex min-w-0 items-start gap-x-3">
                <Checkbox
                  className="mt-0.5 shrink-0"
                  checked={schedule.enabled}
                  disabled={isSaving}
                  onChange={() => handleToggleEnabled(schedule)}
                  aria-label={m.scheduler_aria_toggle({ name: schedule.name })}
                />
                <div className={cx("min-w-0", { "opacity-50": !schedule.enabled })}>
                  <h3 className="truncate text-sm font-semibold text-black dark:text-white">
                    {schedule.name}
                  </h3>
                  <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                    {formatWeekdays(schedule.weekdays)} {m.scheduler_summary_at()}{" "}
                    {formatTime(schedule.hour, schedule.minute)} · {schedule.timezone}
                  </p>
                  <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                    {methodLabel(schedule.method)} → {actionLabel(schedule.action)}
                    {schedule.method === "wol" && schedule.macAddress
                      ? ` · ${schedule.macAddress}`
                      : ""}
                  </p>
                  {nextRun && (
                    <p className="mt-0.5 text-xs text-slate-400 dark:text-slate-500">{nextRun}</p>
                  )}
                  {!schedule.enabled && (
                    <span className="mt-1 inline-block rounded bg-slate-200 px-1.5 py-0.5 text-[10px] font-medium text-slate-600 dark:bg-slate-700 dark:text-slate-300">
                      {m.scheduler_badge_disabled()}
                    </span>
                  )}
                  {stale && schedule.enabled && (
                    <span className="mt-1 inline-block rounded bg-yellow-500 px-1.5 py-0.5 text-[10px] font-medium text-white">
                      {m.scheduler_badge_atx_inactive()}
                    </span>
                  )}
                </div>
              </div>

              <div className="flex shrink-0 items-center gap-1">
                <Button
                  size="XS"
                  theme="light"
                  className="text-red-500 dark:text-red-400"
                  LeadingIcon={LuTrash2}
                  disabled={isSaving}
                  onClick={() => setScheduleToDelete(schedule)}
                  aria-label={m.scheduler_aria_delete({ name: schedule.name })}
                />
                <Button
                  size="XS"
                  theme="light"
                  LeadingIcon={LuPenLine}
                  text={m.scheduler_edit()}
                  disabled={isSaving}
                  onClick={() => setEditing(schedule)}
                  aria-label={m.scheduler_aria_edit({ name: schedule.name })}
                />
              </div>
            </div>
          </Card>
        );
      })}
    </div>
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SettingsPageHeader title={m.scheduler_title()} description={m.scheduler_description()} />
        {schedules.length > 0 && !editing && (
          <div className="flex items-center pl-2">
            <Button
              size="SM"
              theme="primary"
              text={isMaxReached ? m.scheduler_max_reached() : m.scheduler_add()}
              disabled={isMaxReached || isSaving}
              onClick={() => setEditing(emptySchedule(defaultTimezone))}
            />
          </div>
        )}
      </div>

      {loading ? (
        <EmptyCard
          IconElm={LuCalendarClock}
          headline={m.scheduler_loading()}
          BtnElm={
            <div className="my-2 flex flex-col items-center space-y-2 text-center">
              <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
            </div>
          }
        />
      ) : (
        <div className="space-y-4">
          {schedules.length > 0 && scheduleList}

          {editing ? (
            <PowerScheduleForm
              key={editing.id}
              schedule={editing}
              timezones={timezones}
              wolDevices={wolDevices}
              atxAvailable={atxAvailable}
              isSaving={isSaving}
              onSave={handleSave}
              onCancel={() => setEditing(null)}
            />
          ) : (
            schedules.length === 0 && (
              <EmptyCard
                IconElm={LuCalendarClock}
                headline={m.scheduler_empty_headline()}
                description={m.scheduler_empty_description()}
                BtnElm={
                  <Button
                    size="SM"
                    theme="primary"
                    text={m.scheduler_add()}
                    onClick={() => setEditing(emptySchedule(defaultTimezone))}
                  />
                }
              />
            )
          )}
        </div>
      )}

      <ConfirmDialog
        open={scheduleToDelete !== null}
        onClose={() => setScheduleToDelete(null)}
        title={m.scheduler_confirm_delete_title()}
        description={m.scheduler_confirm_delete_description({
          name: scheduleToDelete?.name || "",
        })}
        variant="danger"
        confirmText={m.delete()}
        onConfirm={handleDelete}
        isConfirming={isSaving}
      />
    </div>
  );
}
