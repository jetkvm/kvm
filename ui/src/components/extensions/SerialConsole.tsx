import {
  LuPlus,
  LuTrash2,
  LuPencil,
  LuSettings2,
  LuEye,
  LuEyeOff,
  LuSave,
  LuArrowBigUp,
  LuArrowBigDown,
  LuCircleX,
  LuTerminal,
} from "react-icons/lu";
import { useEffect, useMemo, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { InputFieldWithLabel } from "@components/InputField";
import { useUiStore, useTerminalStore } from "@/hooks/stores";
import Checkbox from "@components/Checkbox";
import { SettingsItem } from "@components/SettingsItem";
import { m } from "@localizations/messages.js";

/** ============== Types ============== */
interface QuickButton {
  id: string; // uuid-ish
  label: string; // shown on the button
  command: string; // raw command to send (without auto-terminator)
  terminator: { label: string; value: string }; // None/CR/LF/CRLF/LFCR
  sort: number; // for stable ordering
}

interface SerialSettings {
  baudRate: number;
  dataBits: number;
  stopBits: string;
  parity: string;
  terminator: { label: string; value: string }; // None/CR/LF/CRLF/LFCR
  hideSerialSettings: boolean;
  enableEcho: boolean; // future use
  normalizeMode: string; // future use
  normalizeLineEnd: string; // future use
  tabRender: string; // future use
  preserveANSI: boolean; // future use
  showNLTag: boolean; // future use
  buttons: QuickButton[];
}

/** ============== Component ============== */

export function SerialConsole() {
  const { setTerminalType } = useUiStore();
  const { setTerminator } = useTerminalStore();

  const { send } = useJsonRpc();

  // extension config (buttons + prefs)
  const [settings, setSettings] = useState<SerialSettings>({
    baudRate: 9600,
    dataBits: 8,
    stopBits: "1",
    parity: "none",
    terminator: { label: "LF (\\n)", value: "\n" },
    hideSerialSettings: false,
    enableEcho: false,
    normalizeMode: "names",
    normalizeLineEnd: "keep",
    tabRender: "",
    preserveANSI: true,
    showNLTag: true,
    buttons: [],
  });

  type NormalizeMode = "caret" | "names" | "hex"; // note: caret (not carret)

  const normalizeHelp: Record<NormalizeMode, string> = {
    caret: "Caret notation: e.g. Ctrl+A as ^A, Esc as ^[",
    names: "Names: e.g. Ctrl+A as <SOH>, Esc as <ESC>",
    hex: "Hex notation: e.g. Ctrl+A as 0x01, Esc as 0x1B",
  };

  // editor modal state
  const [editorOpen, setEditorOpen] = useState<null | { id?: string }>(null);
  const [draftLabel, setDraftLabel] = useState("");
  const [draftCmd, setDraftCmd] = useState("");
  const [draftTerminator, setDraftTerminator] = useState({ label: "LF (\\n)", value: "\n" });

  // load serial settings like SerialConsole
  useEffect(() => {
    send("getSerialSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.serial_console_get_settings_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }

      setSettings(resp.result as SerialSettings);
      setTerminator((resp.result as SerialSettings).terminator.value);
    });
  }, [send, setTerminator]);

  const handleSerialSettingsChange = (config: keyof SerialSettings, value: unknown) => {
    const newSettings = { ...settings, [config]: value };
    send("setSerialSettings", { settings: newSettings }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.serial_console_set_settings_error({
            settings: newSettings,
            error: resp.error.data || m.unknown_error(),
          }),
        );
        return;
      }
    });
    setSettings(newSettings);
  };

  const onClickButton = (btn: QuickButton) => {
    const command = btn.command + btn.terminator.value;

    send("sendCustomCommand", { command }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.serial_console_send_custom_command({
            command: command,
            error: resp.error.data || m.unknown_error(),
          }),
        );
        return;
      }
    });
  };

  /** CRUD helpers */
  const addNew = () => {
    setEditorOpen({ id: undefined });
    setDraftLabel("");
    setDraftCmd("");
    setDraftTerminator({ label: "LF (\\n)", value: "\n" });
  };

  const editBtn = (btn: QuickButton) => {
    setEditorOpen({ id: btn.id });
    setDraftLabel(btn.label);
    setDraftCmd(btn.command);
    setDraftTerminator(btn.terminator);
  };

  const removeBtn = (id: string) => {
    const nextButtons = settings.buttons
      .filter(b => b.id !== id)
      .map((b, i) => ({ ...b, sort: i }));
    handleSerialSettingsChange("buttons", stableSort(nextButtons));
    setEditorOpen(null);
  };

  const moveUpBtn = (id: string) => {
    // Make a copy so we don't mutate state directly
    const newButtons = [...settings.buttons];

    // Find the index of the button to move
    const index = newButtons.findIndex(b => b.id === id);

    if (index > 0) {
      // Swap with the previous element
      [newButtons[index - 1], newButtons[index]] = [newButtons[index], newButtons[index - 1]];
    }

    // Re-assign sort values
    const nextButtons = newButtons.map((b, i) => ({ ...b, sort: i }));
    handleSerialSettingsChange("buttons", stableSort(nextButtons));
    setEditorOpen(null);
  };

  const moveDownBtn = (id: string) => {
    // Make a copy so we don't mutate state directly
    const newButtons = [...settings.buttons];

    // Find the index of the button to move
    const index = newButtons.findIndex(b => b.id === id);

    if (index >= 0 && index < newButtons.length - 1) {
      // Swap with the next element
      [newButtons[index], newButtons[index + 1]] = [newButtons[index + 1], newButtons[index]];
    }

    // Re-assign sort values
    const nextButtons = newButtons.map((b, i) => ({ ...b, sort: i }));
    handleSerialSettingsChange("buttons", stableSort(nextButtons));
    setEditorOpen(null);
  };

  const saveDraft = () => {
    const label = draftLabel.trim() || "Unnamed";
    const command = draftCmd;
    if (!command) {
      notifications.error("Command cannot be empty.");
      return;
    }
    const terminator = draftTerminator;
    console.log("Saving draft:", { label, command, terminator });

    // if editing, get current id, otherwise undefined => new button
    const currentID = editorOpen?.id;

    // either update existing or add new
    // if new, assign next sort index
    // if existing, keep sort index
    const nextButtons = currentID
      ? settings.buttons.map(b => (b.id === currentID ? { ...b, label, command, terminator } : b))
      : [
          ...settings.buttons,
          { id: genId(), label, command, terminator, sort: settings.buttons.length },
        ];

    handleSerialSettingsChange("buttons", stableSort(nextButtons));
    setEditorOpen(null);
  };

  /** simple reordering: alphabetical by sort, then label */
  const sortedButtons = useMemo(() => settings.buttons, [settings.buttons]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.extension_serial_console()}
        description={m.serial_console_configure_description()}
      />

      <Card className="animate-fadeIn opacity-0">
        <div className="space-y-4 p-3">
          {/* Top actions */}
          <div className="flex flex-wrap items-center justify-around gap-3">
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={settings.hideSerialSettings ? LuEye : LuEyeOff}
              text={
                settings.hideSerialSettings
                  ? m.serial_console_show_settings()
                  : m.serial_console_hide_settings()
              }
              onClick={() =>
                handleSerialSettingsChange("hideSerialSettings", !settings.hideSerialSettings)
              }
            />
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={LuPlus}
              text={m.serial_console_add_button()}
              onClick={addNew}
            />
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={LuTerminal}
              text={m.serial_console_open_console()}
              onClick={() => {
                console.log("Opening serial console with settings: ", settings);
                setTerminalType("serial");
              }}
            />
          </div>
          <hr className="border-slate-700/30 dark:border-slate-600/30" />

          {/* Serial settings (collapsible) */}
          {!settings.hideSerialSettings && (
            <>
              <div className="mb-1 grid grid-cols-2 gap-4">
                <SelectMenuBasic
                  label={m.serial_console_baud_rate()}
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
                  value={settings.baudRate}
                  onChange={e => handleSerialSettingsChange("baudRate", Number(e.target.value))}
                />

                <SelectMenuBasic
                  label={m.serial_console_data_bits()}
                  options={[
                    { label: "8", value: "8" },
                    { label: "7", value: "7" },
                  ]}
                  value={settings.dataBits}
                  onChange={e => handleSerialSettingsChange("dataBits", Number(e.target.value))}
                />

                <SelectMenuBasic
                  label={m.serial_console_stop_bits()}
                  options={[
                    { label: "1", value: "1" },
                    { label: "1.5", value: "1.5" },
                    { label: "2", value: "2" },
                  ]}
                  value={settings.stopBits}
                  onChange={e => handleSerialSettingsChange("stopBits", e.target.value)}
                />

                <SelectMenuBasic
                  label={m.serial_console_parity()}
                  options={[
                    { label: m.serial_console_parity_none(), value: "none" },
                    { label: m.serial_console_parity_even(), value: "even" },
                    { label: m.serial_console_parity_odd(), value: "odd" },
                    { label: m.serial_console_parity_mark(), value: "mark" },
                    { label: m.serial_console_parity_space(), value: "space" },
                  ]}
                  value={settings.parity}
                  onChange={e => handleSerialSettingsChange("parity", e.target.value)}
                />
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label={m.serial_console_line_ending()}
                    options={[
                      { label: "None", value: "" },
                      { label: "CR (\\r)", value: "\r" },
                      { label: "LF (\\n)", value: "\n" },
                      { label: "CRLF (\\r\\n)", value: "\r\n" },
                      { label: "LFCR (\\n\\r)", value: "\n\r" },
                    ]}
                    value={settings.terminator.value}
                    onChange={e => {
                      handleSerialSettingsChange("terminator", {
                        label: e.target.selectedOptions[0].text,
                        value: e.target.value,
                      });
                      setTerminator(e.target.value);
                    }}
                  />
                  <div className="mt-0 ml-2 text-xs text-white opacity-70">
                    {m.serial_console_line_ending_explanation({
                      terminator: settings.terminator.label,
                    })}
                  </div>
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label={m.serial_console_normalization_mode()}
                    options={[
                      { label: "Caret", value: "caret" },
                      { label: "Names", value: "names" },
                      { label: "Hex", value: "hex" },
                    ]}
                    value={settings.normalizeMode}
                    onChange={e => {
                      handleSerialSettingsChange("normalizeMode", e.target.value);
                    }}
                  />
                  <div className="mt-0 ml-2 text-xs text-white opacity-70">
                    {normalizeHelp[settings.normalizeMode as NormalizeMode]}
                  </div>
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label={m.serial_console_crlf_handling()}
                    options={[
                      { label: "Keep", value: "keep" },
                      { label: "LF", value: "lf" },
                      { label: "CR", value: "cr" },
                      { label: "CRLF", value: "crlf" },
                      { label: "LFCR", value: "lfcr" },
                    ]}
                    value={settings.normalizeLineEnd}
                    onChange={e => {
                      handleSerialSettingsChange("normalizeLineEnd", e.target.value);
                    }}
                  />
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label={m.serial_console_preserve_ansi()}
                    options={[
                      { label: m.serial_console_preserve_ansi_strip(), value: "strip" },
                      { label: m.serial_console_preserve_ansi_keep(), value: "keep" },
                    ]}
                    value={settings.preserveANSI ? "keep" : "strip"}
                    onChange={e => {
                      handleSerialSettingsChange("preserveANSI", e.target.value === "keep");
                    }}
                  />
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Show newline tag"
                    options={[
                      { label: m.serial_console_show_newline_tag_hide(), value: "hide" },
                      { label: m.serial_console_show_newline_tag_show(), value: "show" },
                    ]}
                    value={settings.showNLTag ? "show" : "hide"}
                    onChange={e => {
                      handleSerialSettingsChange("showNLTag", e.target.value === "show");
                    }}
                  />
                </div>
                <div>
                  <InputFieldWithLabel
                    size="MD"
                    type="text"
                    label={m.serial_console_tab_replacement()}
                    placeholder="ex. spaces, →, |"
                    value={settings.tabRender}
                    onChange={e => {
                      handleSerialSettingsChange("tabRender", e.target.value);
                    }}
                  />
                  <div className="mt-1 text-xs text-white opacity-70">
                    {m.serial_console_tab_replacement_description()}
                  </div>
                </div>
              </div>
              <div className="m-2 space-y-4">
                <SettingsItem
                  title={m.serial_console_local_echo()}
                  description={m.serial_console_local_echo_description()}
                >
                  <Checkbox
                    checked={settings.enableEcho}
                    onChange={e => {
                      handleSerialSettingsChange("enableEcho", e.target.checked);
                    }}
                  />
                </SettingsItem>
              </div>
              <hr className="border-slate-700/30 dark:border-slate-600/30" />
            </>
          )}

          {/* Buttons grid */}
          <div className="grid grid-cols-2 gap-2 pt-2">
            {sortedButtons.map(btn => (
              <div key={btn.id} className="flex min-w-0 items-stretch gap-2">
                <div className="min-w-0 flex-1">
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
              <div className="col-span-2 text-sm text-black dark:text-slate-300">
                No buttons yet. Click “Add Button”.
              </div>
            )}
          </div>

          {/* Editor drawer/modal (inline lightweight) */}
          {editorOpen && (
            <div className="mt-4 rounded-md border bg-slate-50 p-3 dark:bg-slate-900/30">
              <div className="mb-2 flex items-center gap-2">
                <LuSettings2 className="h-3.5 shrink-0 justify-start text-white" />
                <div className="font-medium text-black dark:text-white">
                  {editorOpen.id ? "Edit Button" : "New Button"}
                </div>
              </div>
              <div className="grid h-23 grid-cols-1 gap-3 md:grid-cols-2">
                <div>
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label={m.serial_console_button_editor_label()}
                    placeholder={m.serial_console_button_editor_label_placeholder()}
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
                    label={m.serial_console_button_editor_command()}
                    placeholder={m.serial_console_button_editor_command_placeholder()}
                    value={draftCmd}
                    onChange={e => {
                      setDraftCmd(e.target.value);
                    }}
                  />
                  {draftTerminator.value != "" && (
                    <div className="mt-1 text-xs text-white opacity-70">
                      {m.serial_console_button_editor_explanation({
                        terminator: draftTerminator.label,
                      })}
                    </div>
                  )}
                </div>
              </div>
              <div className="flex items-end justify-around">
                <SelectMenuBasic
                  label={m.serial_console_line_ending()}
                  options={[
                    { label: "None", value: "" },
                    { label: "CR (\\r)", value: "\r" },
                    { label: "LF (\\n)", value: "\n" },
                    { label: "CRLF (\\r\\n)", value: "\r\n" },
                    { label: "LFCR (\\n\\r)", value: "\n\r" },
                  ]}
                  value={draftTerminator.value}
                  onChange={e =>
                    setDraftTerminator({
                      label: e.target.selectedOptions[0].text,
                      value: e.target.value,
                    })
                  }
                />
                <div className="pb-[3px]">
                  <Button
                    size="SM"
                    theme="primary"
                    LeadingIcon={LuSave}
                    text="Save"
                    onClick={saveDraft}
                  />
                </div>
                <div className="pb-[3px]">
                  <Button
                    size="SM"
                    theme="primary"
                    LeadingIcon={LuCircleX}
                    text="Cancel"
                    onClick={() => setEditorOpen(null)}
                  />
                </div>
              </div>
              <div className="mt-3 flex justify-around">
                {editorOpen.id && (
                  <>
                    <Button
                      size="SM"
                      theme="danger"
                      LeadingIcon={LuTrash2}
                      text={m.serial_console_button_editor_delete()}
                      onClick={() => removeBtn(editorOpen.id!)}
                      aria-label={`Delete ${draftLabel}`}
                    />
                    <Button
                      size="SM"
                      theme="primary"
                      LeadingIcon={LuArrowBigUp}
                      text={m.serial_console_button_editor_move_up()}
                      aria-label={`Move ${draftLabel} up`}
                      disabled={sortedButtons.findIndex(b => b.id === editorOpen.id) === 0}
                      onClick={() => moveUpBtn(editorOpen.id!)}
                    />
                    <Button
                      size="SM"
                      theme="primary"
                      LeadingIcon={LuArrowBigDown}
                      text={m.serial_console_button_editor_move_down()}
                      aria-label={`Move ${draftLabel} down`}
                      disabled={
                        sortedButtons.findIndex(b => b.id === editorOpen.id) + 1 ===
                        sortedButtons.length
                      }
                      onClick={() => moveDownBtn(editorOpen.id!)}
                    />
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

/** ============== helpers ============== */
function genId() {
  return "b_" + Math.random().toString(36).slice(2, 10);
}
function stableSort(arr: QuickButton[]) {
  return [...arr].sort((a, b) => a.sort - b.sort || a.label.localeCompare(b.label));
}
