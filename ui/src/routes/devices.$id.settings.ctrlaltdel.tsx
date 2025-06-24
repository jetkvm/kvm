
import { Checkbox } from "@/components/Checkbox";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsCtrlAltDelRoute() {
  const enableCtrlAltDel = useSettingsStore(state => state.actionBarCtrlAltDel);
  const setEnableCtrlAltDel = useSettingsStore(state => state.setActionBarCtrlAltDel);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Action Bar"
        description="Customize the action bar of your JetKVM interface"
      />
      <div className="space-y-4">
        <SettingsItem title="Enable Ctrl-Alt-Del" description="Enable the Ctrl-Alt-Del key on the virtual keyboard">
          <Checkbox
            checked={enableCtrlAltDel}
            onChange={e => setEnableCtrlAltDel(e.target.checked)}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
