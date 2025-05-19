import { useCallback, useEffect } from "react";

import { useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { layouts } from "@/keyboardLayouts";

import { FeatureFlag } from "../components/FeatureFlag";
import { SelectMenuBasic } from "../components/SelectMenuBasic";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsKeyboardRoute() {
  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(
    state => state.setKeyboardLayout,
  );

  const layoutOptions = Object.entries(layouts).map(([code, language]) => { return { value: code, label: language } })

  const [send] = useJsonRpc();

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, []);

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
        description="Configure keyboard layout settings for your device"
      />

      <div className="space-y-4">
        <FeatureFlag minAppVersion="0.4.0" name="Paste text">
	  { /* this menu item could be renamed to plain "Keyboard layout" in the future, when also the virtual keyboard layout mappings are being implemented */ }
          <SettingsItem
            title="Paste text"
            description="Keyboard layout of target operating system"
          >
            <SelectMenuBasic
              size="SM"
              label=""
              fullWidth
              value={keyboardLayout}
              onChange={onKeyboardLayoutChange}
              options={layoutOptions}
            />
          </SettingsItem>
	  <p className="text-xs text-slate-600 dark:text-slate-400">
	    Pasting text sends individual key strokes to the target device. The keyboard layout determines which key codes are being sent. Ensure that the keyboard layout in JetKVM matches the settings in the operating system.
          </p>
        </FeatureFlag>
      </div>
    </div>
  );
}
