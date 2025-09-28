import { useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();

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
            t('Failed_to_set_keyboard_layout_msg',{msg:resp.error.data || t('Unknown_error')})
          );
        }
        notifications.success(t('Keyboard_layout_set_successfully_to_code',{code:isoCode}));
        setKeyboardLayout(isoCode);
      });
    },
    [send, setKeyboardLayout],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Keyboard')}
        description={t('Configure_keyboard_settings_for_your_device')}
      />

      <div className="space-y-4">
        <SettingsItem
          title={t('Keyboard_Layout')}
          description={t('Keyboard_layout_of_target_operating_system')}
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
            {t('keyboard_layout_notice')}
        </p>
      </div>

      <div className="space-y-4">
        <SettingsItem
          title={t('Show_Pressed_Keys')}
          description={t('Display_currently_pressed_keys_in_the_status_bar')}
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
