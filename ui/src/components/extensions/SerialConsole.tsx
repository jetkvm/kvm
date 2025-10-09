import { LuPlus, LuTrash2, LuPencil, LuSettings2, LuEye, LuEyeOff, LuSave, LuArrowBigUp, LuArrowBigDown, LuCircleX, LuTerminal } from "react-icons/lu";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { InputFieldWithLabel } from "@components/InputField";
import { useUiStore, useTerminalStore } from "@/hooks/stores";
import Checkbox from "@components/Checkbox";
import {SettingsItem} from "@components/SettingsItem";



/** ============== Types ============== */
interface QuickButton {
  id: string;         // uuid-ish
  label: string;      // shown on the button
  command: string;    // raw command to send (without auto-terminator)
  terminator: {label: string, value: string}; // None/CR/LF/CRLF/LFCR
  sort: number;       // for stable ordering
}

interface SerialSettings {
  baudRate: number;
  dataBits: number;
  stopBits: string;
  parity: string;
  terminator: {label: string, value: string}; // None/CR/LF/CRLF/LFCR
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
  const [buttonConfig, setButtonConfig] = useState<SerialSettings>({
    baudRate: 9600,
    dataBits: 8,
    stopBits: "1",
    parity: "none",
    terminator: {label: "LF (\\n)", value: "\n"},
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
    hex:   "Hex notation: e.g. Ctrl+A as 0x01, Esc as 0x1B",
  };

  // editor modal state
  const [editorOpen, setEditorOpen] = useState<null | { id?: string }>(null);
  const [draftLabel, setDraftLabel] = useState("");
  const [draftCmd, setDraftCmd] = useState("");
  const [draftTerminator, setDraftTerminator] = useState({label: "LF (\\n)", value: "\n"});

  // load serial settings like SerialConsole
  useEffect(() => {
    send("getSerialSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get button config: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      setButtonConfig(resp.result as SerialSettings);
      setTerminator((resp.result as SerialSettings).terminator.value);
    });

  }, [send, setTerminator]);

  const handleSerialSettingsChange = (config: keyof SerialSettings, value: unknown) => {
    const newButtonConfig = { ...buttonConfig, [config]: value };
    send("setSerialSettings", { settings: newButtonConfig }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to update serial settings: ${resp.error.data || "Unknown error"}`);
        return;
      }
    });
    setButtonConfig(newButtonConfig);
  };

  const onClickButton = (btn: QuickButton) => {

    const command = btn.command + btn.terminator.value;

    send("sendCustomCommand", { command }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to send custom command: ${resp.error.data || "Unknown error"}`,
        );
      }
    });
  };

  /** CRUD helpers */
  const addNew = () => {
    setEditorOpen({ id: undefined });
    setDraftLabel("");
    setDraftCmd("");
    setDraftTerminator({label: "LF (\\n)", value: "\n"});
  };

  const editBtn = (btn: QuickButton) => {
    setEditorOpen({ id: btn.id });
    setDraftLabel(btn.label);
    setDraftCmd(btn.command);
    setDraftTerminator(btn.terminator);
  };

  const removeBtn = (id: string) => {
    const nextButtons = buttonConfig.buttons.filter(b => b.id !== id).map((b, i) => ({ ...b, sort: i })) ;
    handleSerialSettingsChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  const moveUpBtn = (id: string) => {
    // Make a copy so we don't mutate state directly
    const newButtons = [...buttonConfig.buttons];

    // Find the index of the button to move
    const index = newButtons.findIndex(b => b.id === id);

    if (index > 0) {
      // Swap with the previous element
      [newButtons[index - 1], newButtons[index]] = [
        newButtons[index],
        newButtons[index - 1],
      ];
    }

    // Re-assign sort values
    const nextButtons = newButtons.map((b, i) => ({ ...b, sort: i }));
    handleSerialSettingsChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  const moveDownBtn = (id: string) => {
    // Make a copy so we don't mutate state directly
    const newButtons = [...buttonConfig.buttons];

    // Find the index of the button to move
    const index = newButtons.findIndex(b => b.id === id);

    if (index >= 0 && index < newButtons.length - 1) {
      // Swap with the next element
      [newButtons[index], newButtons[index + 1]] = [
        newButtons[index + 1],
        newButtons[index],
      ];
    }

    // Re-assign sort values
    const nextButtons = newButtons.map((b, i) => ({ ...b, sort: i }));
    handleSerialSettingsChange("buttons", stableSort(nextButtons) );
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
      ? buttonConfig.buttons.map(b => (b.id === currentID ? { ...b, label, command , terminator} : b))
      : [...buttonConfig.buttons, { id: genId(), label, command, terminator, sort: buttonConfig.buttons.length }];

    handleSerialSettingsChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  /** simple reordering: alphabetical by sort, then label */
  const sortedButtons = useMemo(() => buttonConfig.buttons, [buttonConfig.buttons]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Serial Console"
        description="Configure your serial console settings and create quick command buttons"

      />

      <Card className="animate-fadeIn opacity-0">
        <div className="space-y-4 p-3">
          {/* Top actions */}
          <div className="flex flex-wrap justify-around items-center gap-3">
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={buttonConfig.hideSerialSettings ? LuEye : LuEyeOff}
              text={buttonConfig.hideSerialSettings ? "Show Settings" : "Hide Settings"}
              onClick={() => handleSerialSettingsChange("hideSerialSettings", !buttonConfig.hideSerialSettings )}
            />
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={LuPlus}
              text="Add Button"
              onClick={addNew}
            />
            <Button
              size="XS"
              theme="primary"
              LeadingIcon={LuTerminal}
              text="Open Console"
              onClick={() => {
                setTerminalType("serial");
                console.log("Opening serial console with settings: ", buttonConfig);
              }}
            />
          </div>
          <hr className="border-slate-700/30 dark:border-slate-600/30" />

          {/* Serial settings (collapsible) */}
          {!buttonConfig.hideSerialSettings && (
            <>
              <div className="grid grid-cols-2 gap-4 mb-1">
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
                  value={buttonConfig.baudRate}
                  onChange={(e) => handleSerialSettingsChange("baudRate", Number(e.target.value))}
                />

                <SelectMenuBasic
                  label="Data Bits"
                  options={[
                    { label: "8", value: "8" },
                    { label: "7", value: "7" },
                  ]}
                  value={buttonConfig.dataBits}
                  onChange={(e) => handleSerialSettingsChange("dataBits", Number(e.target.value))}
                />

                <SelectMenuBasic
                  label="Stop Bits"
                  options={[
                    { label: "1", value: "1" },
                    { label: "1.5", value: "1.5" },
                    { label: "2", value: "2" },
                  ]}
                  value={buttonConfig.stopBits}
                  onChange={(e) => handleSerialSettingsChange("stopBits", e.target.value)}
                />

                <SelectMenuBasic
                  label="Parity"
                  options={[
                    { label: "None", value: "none" },
                    { label: "Even", value: "even" },
                    { label: "Odd", value: "odd" },
                  ]}
                  value={buttonConfig.parity}
                  onChange={(e) => handleSerialSettingsChange("parity", e.target.value)}
                />
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Line ending"
                    options={[
                      { label: "None", value: "" },
                      { label: "CR (\\r)", value: "\r" },
                      { label: "LF (\\n)", value: "\n" },
                      { label: "CRLF (\\r\\n)", value: "\r\n" },
                      { label: "LFCR (\\n\\r)", value: "\n\r" },
                    ]}
                    value={buttonConfig.terminator.value}
                    onChange={(e) => {
                      handleSerialSettingsChange("terminator", {label: e.target.selectedOptions[0].text, value: e.target.value})
                      setTerminator(e.target.value);
                    }}
                  />
                  <div className="text-xs text-white opacity-70 mt-0 ml-2">
                    When sent, the selected line ending ({buttonConfig.terminator.label}) will be appended.
                  </div>
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Normalization Mode"
                    options={[
                      { label: "Caret", value: "caret" },
                      { label: "Names", value: "names" },
                      { label: "Hex", value: "hex" },
                    ]}
                    value={buttonConfig.normalizeMode}
                    onChange={(e) => {
                      handleSerialSettingsChange("normalizeMode", e.target.value)
                    }}
                  />
                  <div className="text-xs text-white opacity-70 mt-0 ml-2">
                    {normalizeHelp[(buttonConfig.normalizeMode as NormalizeMode)]}
                  </div>
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="CRLF Handling"
                    options={[
                      { label: "Keep", value: "keep" },
                      { label: "LF", value: "lf" },
                      { label: "CR", value: "cr" },
                      { label: "CRLF", value: "crlf" },
                      { label: "LFCR", value: "lfcr" },
                    ]}
                    value={buttonConfig.normalizeLineEnd}
                    onChange={(e) => {
                      handleSerialSettingsChange("normalizeLineEnd", e.target.value)
                    }}
                  />
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Preserve ANSI"
                    options={[
                      { label: "Strip escape code", value: "strip" },
                      { label: "Keep escape code", value: "keep" },
                    ]}
                    value={buttonConfig.preserveANSI ? "keep" : "strip"}
                    onChange={(e) => {
                      handleSerialSettingsChange("preserveANSI", e.target.value === "keep")
                    }}
                  />
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Show newline tag"
                    options={[
                      { label: "Hide <LF> tag", value: "hide" },
                      { label: "Show <LF> tag", value: "show" },
                    ]}
                    value={buttonConfig.showNLTag ? "show" : "hide"}
                    onChange={(e) => {
                      handleSerialSettingsChange("showNLTag", e.target.value === "show")
                    }}
                  />
                </div>
                <div>
                  <InputFieldWithLabel
                    size="MD"
                    type="text"
                    label="Tab replacement"
                    placeholder="ex. spaces, →, |"
                    value={buttonConfig.tabRender}
                    onChange={e => {
                      handleSerialSettingsChange("tabRender", e.target.value)
                    }}
                  />
                  <div className="text-xs text-white opacity-70 mt-1">
                    Empty for no replacement
                  </div>
                </div>
              </div>
              <div className="space-y-4 m-2">
                <SettingsItem
                  title="Local Echo"
                  description="Whether to echo received characters back to the sender"
                >
                  <Checkbox
                    checked={buttonConfig.enableEcho}
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
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3 h-23">
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
                  {draftTerminator.value != "" && (
                    <div className="text-xs text-white opacity-70 mt-1">
                      When sent, the selected line ending ({draftTerminator.label}) will be appended.
                  </div>
                  )}
                </div>
              </div>
              <div className="flex justify-around items-end">
                <SelectMenuBasic
                  label="Line ending"
                  options={[
                    { label: "None", value: "" },
                    { label: "CR (\\r)", value: "\r" },
                    { label: "LF (\\n)", value: "\n" },
                    { label: "CRLF (\\r\\n)", value: "\r\n" },
                    { label: "LFCR (\\n\\r)", value: "\n\r" },
                  ]}
                  value={draftTerminator.value}
                  onChange={(e) => setDraftTerminator({label: e.target.selectedOptions[0].text, value: e.target.value})}
                />
                <div className="pb-[3px]">
                  <Button size="SM" theme="primary" LeadingIcon={LuSave} text="Save" onClick={saveDraft} />
                </div>
                <div className="pb-[3px]">
                  <Button size="SM" theme="primary" LeadingIcon={LuCircleX} text="Cancel" onClick={() => setEditorOpen(null)} />
                </div>
              </div>
              <div className="flex justify-around mt-3">
                {editorOpen.id && (
                  <>
                    <Button
                      size="SM"
                      theme="danger"
                      LeadingIcon={LuTrash2}
                      text="Delete"
                      onClick={() => removeBtn(editorOpen.id!)}
                      aria-label={`Delete ${draftLabel}`}
                    />
                    <Button
                      size="SM"
                      theme="primary"
                      LeadingIcon={LuArrowBigUp}
                      text="Move Up"
                      aria-label={`Move ${draftLabel} up`}
                      disabled={sortedButtons.findIndex(b => b.id === editorOpen.id) === 0}
                      onClick={() => moveUpBtn(editorOpen.id!)}
                    />
                    <Button
                      size="SM"
                      theme="primary"
                      LeadingIcon={LuArrowBigDown}
                      text="Move Down"
                      aria-label={`Move ${draftLabel} down`}
                      disabled={sortedButtons.findIndex(b => b.id === editorOpen.id)+1 === sortedButtons.length}
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
  return [...arr].sort((a, b) => (a.sort - b.sort) || a.label.localeCompare(b.label));
}

