import { useEffect, useState } from "react";

import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { m } from "@localizations/messages.js";

export interface AbsMouseMapping {
  enabled: boolean;
  total_width: number;
  total_height: number;
  screen_x: number;
  screen_y: number;
  screen_width: number;
  screen_height: number;
}

export const defaultAbsMouseMapping: AbsMouseMapping = {
  enabled: false,
  total_width: 0,
  total_height: 0,
  screen_x: 0,
  screen_y: 0,
  screen_width: 0,
  screen_height: 0,
};

export function MouseMappingSetting({
  mapping,
  onSave,
}: {
  mapping: AbsMouseMapping;
  onSave: (mapping: AbsMouseMapping) => void;
}) {
  const [state, setState] = useState<AbsMouseMapping>(mapping);

  useEffect(() => {
    setState(mapping);
  }, [mapping]);

  const numberField = (key: keyof Omit<AbsMouseMapping, "enabled">, label: string, min: number) => (
    <InputFieldWithLabel
      required
      size="SM"
      label={label}
      value={state[key]}
      type="number"
      min={String(min)}
      onChange={e => {
        // Guard the controlled input against NaN (empty/partial input -> 0,
        // which the device-side validation rejects with a clear error).
        const n = Number(e.target.value);
        setState({ ...state, [key]: Number.isFinite(n) ? Math.trunc(n) : 0 });
      }}
    />
  );

  return (
    <div className="space-y-4">
      <p className="text-xs text-slate-600 dark:text-slate-400">{m.mouse_multimonitor_hint()}</p>
      <div className="grid grid-cols-1 items-end gap-4 md:grid-cols-2">
        {numberField("total_width", m.mouse_multimonitor_total_width_label(), 1)}
        {numberField("total_height", m.mouse_multimonitor_total_height_label(), 1)}
        {numberField("screen_x", m.mouse_multimonitor_screen_x_label(), 0)}
        {numberField("screen_y", m.mouse_multimonitor_screen_y_label(), 0)}
        {numberField("screen_width", m.mouse_multimonitor_screen_width_label(), 1)}
        {numberField("screen_height", m.mouse_multimonitor_screen_height_label(), 1)}
      </div>
      <div className="flex gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text={m.mouse_multimonitor_save()}
          onClick={() => onSave({ ...state, enabled: true })}
        />
      </div>
    </div>
  );
}
