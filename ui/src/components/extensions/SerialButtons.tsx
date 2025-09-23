import { LuPlus, LuTrash2, LuPencil, LuSettings2, LuEye, LuEyeOff, LuSave } from "react-icons/lu";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { InputFieldWithLabel } from "@components/InputField";

/** ============== Types ============== */

interface SerialSettings {
  baudRate: string;
  dataBits: string;
  stopBits: string;
  parity: string;
}

interface QuickButton {
  id: string;         // uuid-ish
  label: string;      // shown on the button
  command: string;    // raw command to send (without auto-terminator)
  sort: number;       // for stable ordering
}

interface ButtonConfig {
  buttons: QuickButton[];
  terminator: string;   // CR/CRLF/None
  hideSerialSettings: boolean;
}

/** ============== Component ============== */

export function SerialButtons() {
  const { send } = useJsonRpc();

  // serial settings (same as SerialConsole)
  const [serialSettings, setSerialSettings] = useState<SerialSettings>({
    baudRate: "9600",
    dataBits: "8",
    stopBits: "1",
    parity: "none",
  });

  // extension config (buttons + prefs)
  const [buttonConfig, setButtonConfig] = useState<ButtonConfig>({
  buttons: [],
  terminator: "",
  hideSerialSettings: false,
});

  // editor modal state
  const [editorOpen, setEditorOpen] = useState<null | { id?: string }>(null);
  const [draftLabel, setDraftLabel] = useState("");
  const [draftCmd, setDraftCmd] = useState("");

  // load serial settings like SerialConsole
  useEffect(() => {
    send("getSerialSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get serial settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setSerialSettings(resp.result as SerialSettings);
    });

    send("getSerialButtonConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get button config: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      setButtonConfig(resp.result as ButtonConfig);
    });

  });

  const handleSerialSettingChange = (setting: keyof SerialSettings, value: string) => {
    const newSettings = { ...serialSettings, [setting]: value };
    send("setSerialSettings", { settings: newSettings }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to update serial settings: ${resp.error.data || "Unknown error"}`);
        return;
      }
      setSerialSettings(newSettings);
    });
  };

  const handleSerialButtonConfigChange = (config: keyof ButtonConfig, value: unknown) => {
    const newButtonConfig = { ...buttonConfig, [config]: value };
    send("setSerialButtonConfig", { config: newButtonConfig }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to update button config: ${resp.error.data || "Unknown error"}`);
        return;
      }
      setButtonConfig(newButtonConfig);
    });
  };

  const onClickButton = (btn: QuickButton) => {

    /** build final string to send:
     *  if the user's button command already contains a terminator, we don't append the selected terminator safely
     */
    const raw = btn.command;
    const t = buttonConfig.terminator ?? "";
    const command = raw.endsWith("\r") || raw.endsWith("\n") ? raw : raw + t;

    send("sendCustomCommand", { command }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to send ATX power action: ${resp.error.data || "Unknown error"}`,
        );
      }
    });

  };

  /** CRUD helpers */
  const addNew = () => {
    setEditorOpen({ id: undefined });
    setDraftLabel("");
    setDraftCmd("");
  };

  const editBtn = (btn: QuickButton) => {
    setEditorOpen({ id: btn.id });
    setDraftLabel(btn.label);
    setDraftCmd(btn.command);
  };

  const removeBtn = (id: string) => {
    const nextButtons = buttonConfig.buttons.filter(b => b.id !== id).map((b, i) => ({ ...b, sort: i })) ;
    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  const saveDraft = () => {
    const label = draftLabel.trim() || "Unnamed";
    const command = draftCmd.trim();
    if (!command) {
      notifications.error("Command cannot be empty.");
      return;
    }

    const isEdit = editorOpen?.id;
    const nextButtons = isEdit
      ? buttonConfig.buttons.map(b => (b.id === isEdit ? { ...b, label, command } : b))
      : [...buttonConfig.buttons, { id: genId(), label, command, sort: buttonConfig.buttons.length }];

    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  /** simple reordering: alphabetical by sort, then label */
  const sortedButtons = useMemo(() => stableSort(buttonConfig.buttons), [buttonConfig.buttons]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Serial Buttons"
        description="Quick custom commands over the extension serial port"
      />

      <Card className="animate-fadeIn opacity-0">
        <div className="space-y-4 p-3">
          {/* Top actions */}
          <div className="flex flex-wrap justify-around items-center gap-3">
            <Button
              size="SM"
              theme="primary"
              LeadingIcon={buttonConfig.hideSerialSettings ? LuEye : LuEyeOff}
              text={buttonConfig.hideSerialSettings ? "Show Serial Settings" : "Hide Serial Settings"}
              onClick={() => handleSerialButtonConfigChange("hideSerialSettings", !buttonConfig.hideSerialSettings )}
            />
            <Button
              size="SM"
              theme="primary"
              LeadingIcon={LuPlus}
              text="Add Button"
              onClick={addNew}
            />
          </div>

          {/* Serial settings (collapsible) */}
          {!buttonConfig.hideSerialSettings && (
            <>
              <hr className="border-slate-700/30 dark:border-slate-600/30" />
              <div className="grid grid-cols-2 gap-4">
                <SelectMenuBasic
                  label="Baud Rate"
                  options={[
                    { label: "1200", value: "1200" },
                    { label: "2400", value: "2400" },
                    { label: "4800", value: "4800" },
                    { label: "9600", value: "9600" },
                    { label: "19200", value: "19200" },
                    { label: "38400", value: "38400" },
                    { label: "57600", value: "57600" },
                    { label: "115200", value: "115200" },
                  ]}
                  value={serialSettings.baudRate}
                  onChange={(e) => handleSerialSettingChange("baudRate", e.target.value)}
                />

                <SelectMenuBasic
                  label="Data Bits"
                  options={[
                    { label: "8", value: "8" },
                    { label: "7", value: "7" },
                  ]}
                  value={serialSettings.dataBits}
                  onChange={(e) => handleSerialSettingChange("dataBits", e.target.value)}
                />

                <SelectMenuBasic
                  label="Stop Bits"
                  options={[
                    { label: "1", value: "1" },
                    { label: "1.5", value: "1.5" },
                    { label: "2", value: "2" },
                  ]}
                  value={serialSettings.stopBits}
                  onChange={(e) => handleSerialSettingChange("stopBits", e.target.value)}
                />

                <SelectMenuBasic
                  label="Parity"
                  options={[
                    { label: "None", value: "none" },
                    { label: "Even", value: "even" },
                    { label: "Odd", value: "odd" },
                  ]}
                  value={serialSettings.parity}
                  onChange={(e) => handleSerialSettingChange("parity", e.target.value)}
                />
              </div>
              <SelectMenuBasic
                  label="Line ending"
                  options={[
                    { label: "None", value: "none" },
                    { label: "CR (\\r)", value: "\r" },
                    { label: "CRLF (\\r\\n)", value: "\r\n" },
                  ]}
                  value={buttonConfig.terminator}
                  onChange={(e) => handleSerialButtonConfigChange("terminator", e.target.value)}
                />
              <hr className="border-slate-700/30 dark:border-slate-600/30" />
            </>
          )}

          {/* Buttons grid */}
          <div className="grid grid-cols-2 gap-2 pt-2">
            {sortedButtons.map((btn) => (
              <div key={btn.id} className="flex items-stretch gap-2 min-w-0">
                <div className=" flex-1  min-w-0 ">
                  <Button
                    size="MD"
                    fullWidth
                    className="overflow-hidden text-ellipsis whitespace-nowrap"
                    theme="primary"
                    text={btn.label}
                    onClick={() => onClickButton(btn)}
                  />
                </div>
                <Button
                  size="MD"
                  theme="light"
                  className="shrink-0"
                  LeadingIcon={LuPencil}
                  onClick={() => editBtn(btn)}
                  aria-label={`Edit ${btn.label}`}
                />
              </div>
            ))}
            {sortedButtons.length === 0 && (
              <div className="col-span-2 text-sm text-black dark:text-slate-300">No buttons yet. Click “Add Button”.</div>
            )}
          </div>

          {/* Editor drawer/modal (inline lightweight) */}
          {editorOpen && (
            <div className="mt-4 border rounded-md p-3 bg-slate-50 dark:bg-slate-900/30">
              <div className="flex items-center gap-2 mb-2">
                <LuSettings2 className="h-3.5 text-white shrink-0 justify-start" />
                <div className="font-medium text-black dark:text-white">{editorOpen.id ? "Edit Button" : "New Button"}</div>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div>
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label="Label"
                    placeholder="New Command"
                    value={draftLabel}
                    onChange={e => {
                      setDraftLabel(e.target.value);
                    }}
                  />
                </div>
                <div>
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label="Command"
                    placeholder="Command to send"
                    value={draftCmd}
                    onChange={e => {
                      setDraftCmd(e.target.value);
                    }}
                  />
                  <div className="text-xs text-white opacity-70 mt-1">
                    The selected line ending ({pretty(buttonConfig.terminator)}) will be appended when sent.
                  </div>
                </div>
              </div>
              <div className="flex gap-2 mt-3">
                <Button size="SM" theme="primary" LeadingIcon={LuSave} text="Save" onClick={saveDraft} />
                <Button size="SM" theme="primary" text="Cancel" onClick={() => setEditorOpen(null)} />
                {editorOpen.id &&(<Button
                  size="SM"
                  theme="danger"
                  LeadingIcon={LuTrash2}
                  text="Delete"
                  onClick={() => removeBtn(editorOpen.id!)}
                  aria-label={`Delete ${draftLabel}`}
                />)}
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

/** ============== helpers ============== */

function pretty(s: string) {
  return s.replace(/\r/g, "\\r").replace(/\n/g, "\\n");
}
function genId() {
  return "b_" + Math.random().toString(36).slice(2, 10);
}
function stableSort(arr: QuickButton[]) {
  return [...arr].sort((a, b) => (a.sort - b.sort) || a.label.localeCompare(b.label));
}

