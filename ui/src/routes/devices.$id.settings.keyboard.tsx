import { useCallback, useEffect } from "react";

import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { Checkbox } from "@/components/Checkbox";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import notifications from "@/notifications";

import { SettingsItem } from "./devices.$id.settings";

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
            `Failed to set keyboard layout: ${resp.error.data || "Unknown error"}`,
          );
        }
        notifications.success("Keyboard layout set successfully to " + isoCode);
        setKeyboardLayout(isoCode);
      });
    },
    [send, setKeyboardLayout],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Keyboard"
        description="Configure keyboard settings for your device"
      />

      <div className="space-y-4">
        <SettingsItem
          title="Paste text"
          description="Keyboard layout of target operating system"
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
          Pasting text sends individual key strokes to the target device. The keyboard layout determines which key codes are being sent. Ensure that the keyboard layout in JetKVM matches the settings in the operating system.
        </p>
      </div>

      <div className="space-y-4">
        <SettingsItem
          title="Show Pressed Keys"
          description="Display currently pressed keys in the status bar"
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
