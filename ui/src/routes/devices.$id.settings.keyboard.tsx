import { useCallback, useEffect, useMemo } from "react";

import { KeyboardLedSync, useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { keyboardOptions } from "@/keyboardLayouts";
import { Checkbox } from "@/components/Checkbox";

import { SelectMenuBasic } from "../components/SelectMenuBasic";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsKeyboardRoute() {
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const showPressedKeys = useSettingsStore(state => state.showPressedKeys);
  const setKeyboardLayout = useSettingsStore(
    state => state.setKeyboardLayout,
  );
  const setKeyboardLedSync = useSettingsStore(
    state => state.setKeyboardLedSync,
  );
  const setShowPressedKeys = useSettingsStore(
    state => state.setShowPressedKeys,
  );

  // this ensures we always get the original en_US if it hasn't been set yet
  const safeKeyboardLayout = useMemo(() => {
      if (keyboardLayout && keyboardLayout.length > 0)
        return keyboardLayout;
      return "en_US";
  }, [keyboardLayout]);

  const layoutOptions = keyboardOptions();
  const ledSyncOptions = [
    { value: "auto", label: "Automatic" },
    { value: "browser", label: "Browser Only" },
    { value: "host", label: "Host Only" },
  ];

  const [send] = useJsonRpc();

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const onKeyboardLayoutChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const layout = e.target.value;
      send("setKeyboardLayout", { layout }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set keyboard layout: ${resp.error.data || "Unknown error"}`,
          );
        }
        notifications.success("Keyboard layout set successfully");
        setKeyboardLayout(layout);
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
        { /* this menu item could be renamed to plain "Keyboard layout" in the future, when also the virtual keyboard layout mappings are being implemented */ }
        <SettingsItem
          title="Paste text"
          description="Keyboard layout of target operating system"
        >
          <SelectMenuBasic
            size="SM"
            label=""
            fullWidth
            value={safeKeyboardLayout}
            onChange={onKeyboardLayoutChange}
            options={layoutOptions}
          />
        </SettingsItem>
        <p className="text-xs text-slate-600 dark:text-slate-400">
          Pasting text sends individual key strokes to the target device. The keyboard layout determines which key codes are being sent. Ensure that the keyboard layout in JetKVM matches the settings in the operating system.
        </p>
      </div>

      <div className="space-y-4">
        { /* this menu item could be renamed to plain "Keyboard layout" in the future, when also the virtual keyboard layout mappings are being implemented */ }
        <SettingsItem
          title="LED state synchronization"
          description="Synchronize the LED state of the keyboard with the target device"
        >
          <SelectMenuBasic
            size="SM"
            label=""
            fullWidth
            value={keyboardLedSync}
            onChange={e => setKeyboardLedSync(e.target.value as KeyboardLedSync)}
            options={ledSyncOptions}
          />
        </SettingsItem>
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
