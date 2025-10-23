import { useCallback, useEffect } from "react";

import { useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import useKeyboardLayout from "@hooks/useKeyboardLayout";
import { Checkbox } from "@components/Checkbox";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export default function SettingsKeyboardRoute() {
  const { setKeyboardLayout } = useSettingsStore();
  const { showPressedKeys, setShowPressedKeys } = useSettingsStore();
  const { selectedKeyboard, keyboardOptions } = useKeyboardLayout();

  const { send } = useJsonRpc();

  useEffect(() => {
    send("getKeyboardLayout", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const isoCode = resp.result as string;
      console.log("Fetched keyboard layout from backend:", isoCode);
      if (isoCode && isoCode.length > 0) {
        setKeyboardLayout(isoCode);
      }
    });
  }, [send, setKeyboardLayout]);

  const onKeyboardLayoutChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const isoCode = e.target.value;
      send("setKeyboardLayout", { layout: isoCode }, resp => {
        if ("error" in resp) {
          notifications.error(
            m.keyboard_layout_error({ error: resp.error.data || m.unknown_error() }),
          );
        }
        notifications.success(m.keyboard_layout_success({ layout: isoCode }));
        setKeyboardLayout(isoCode);
      });
    },
    [send, setKeyboardLayout],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.keyboard_title()}
        description={m.keyboard_description()}
      />

      <div className="space-y-4">
        <SettingsItem
          title={m.keyboard_layout_title()}
          description={m.keyboard_layout_description()}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            fullWidth
            value={selectedKeyboard.isoCode}
            onChange={onKeyboardLayoutChange}
            options={keyboardOptions}
          />
        </SettingsItem>
        <p className="text-xs text-slate-600 dark:text-slate-400">
          {m.keyboard_layout_long_description()}
        </p>
      </div>

      <div className="space-y-4">
        <SettingsItem
          title={m.keyboard_show_pressed_keys_title()}
          description={m.keyboard_show_pressed_keys_description()}
        >
          <Checkbox
            checked={showPressedKeys}
            onChange={e => setShowPressedKeys(e.target.checked)}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
