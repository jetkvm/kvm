import { LuPlus, LuTrash2, LuPencil, LuSettings2, LuEye, LuEyeOff, LuSave, LuArrowBigUp, LuArrowBigDown, LuCircleX, LuTerminal } from "react-icons/lu";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { InputFieldWithLabel } from "@components/InputField";
import { useUiStore } from "@/hooks/stores";

import Checkbox from "../../components/Checkbox";
import { SettingsItem } from "../../routes/devices.$id.settings";



/** ============== Types ============== */
interface QuickButton {
  id: string;         // uuid-ish
  label: string;      // shown on the button
  command: string;    // raw command to send (without auto-terminator)
  terminator: {label: string, value: string}; // None/CR/LF/CRLF/LFCR
  sort: number;       // for stable ordering
}

interface CustomButtonSettings {
  baudRate: string;
  dataBits: string;
  stopBits: string;
  parity: string;
  terminator: {label: string, value: string}; // None/CR/LF/CRLF/LFCR
  lineMode: boolean;
  hideSerialSettings: boolean;
  enableEcho: boolean; // future use
  buttons: QuickButton[];
}

/** ============== Component ============== */

export function SerialButtons() {
  const { setTerminalType, setTerminalLineMode } = useUiStore();

  // This will receive all JSON-RPC notifications (method + no id)
  const { send } = useJsonRpc((payload) => {
    if (payload.method !== "serial.rx") return;
    // if (paused) return;

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const p = payload.params as any;
    let chunk = "";

    if (typeof p?.base64 === "string") {
      try {
        chunk = atob(p.base64);
      } catch {
        // ignore malformed base64
      }
    } else if (typeof p?.data === "string") {
      // fallback if you ever send plain text
      chunk = p.data;
    }

    if (!chunk) return;

    // Normalize CRLF for display
    chunk = chunk.replace(/\r\n/g, "\n");

    // setSerialResponse(prev => (prev + chunk).slice(-MAX_CHARS));
  });

  // extension config (buttons + prefs)
  const [buttonConfig, setButtonConfig] = useState<CustomButtonSettings>({
    baudRate: "9600",
    dataBits: "8",
    stopBits: "1",
    parity: "none",
    terminator: {label: "CR (\\r)", value: "\r"},
    lineMode: true,
    hideSerialSettings: false,
    enableEcho: false,
    buttons: [],
  });

  // editor modal state
  const [editorOpen, setEditorOpen] = useState<null | { id?: string }>(null);
  const [draftLabel, setDraftLabel] = useState("");
  const [draftCmd, setDraftCmd] = useState("");
  const [draftTerminator, setDraftTerminator] = useState({label: "CR (\\r)", value: "\r"});

  // load serial settings like SerialConsole
  useEffect(() => {
    send("getSerialButtonConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get button config: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      setButtonConfig(resp.result as CustomButtonSettings);
      setTerminalLineMode((resp.result as CustomButtonSettings).lineMode);
    });

  }, [send, setTerminalLineMode]);

  const handleSerialButtonConfigChange = (config: keyof CustomButtonSettings, value: unknown) => {
    const newButtonConfig = { ...buttonConfig, [config]: value };
    send("setSerialButtonConfig", { config: newButtonConfig }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to update button config: ${resp.error.data || "Unknown error"}`);
        return;
      }
    });
    setButtonConfig(newButtonConfig);
  };

  const onClickButton = (btn: QuickButton) => {

    const command = btn.command + btn.terminator.value;
    const terminator = btn.terminator.value;

    send("sendCustomCommand", { command, terminator }, (resp: JsonRpcResponse) => {
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
    setDraftTerminator({label: "CR (\\r)", value: "\r"});
  };

  const editBtn = (btn: QuickButton) => {
    setEditorOpen({ id: btn.id });
    setDraftLabel(btn.label);
    setDraftCmd(btn.command);
    setDraftTerminator(btn.terminator);
  };

  const removeBtn = (id: string) => {
    const nextButtons = buttonConfig.buttons.filter(b => b.id !== id).map((b, i) => ({ ...b, sort: i })) ;
    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
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
    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
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
    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
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

    // if editing, get current id, otherwise undefined => new button
    const currentID = editorOpen?.id;

    // either update existing or add new
    // if new, assign next sort index
    // if existing, keep sort index
    const nextButtons = currentID
      ? buttonConfig.buttons.map(b => (b.id === currentID ? { ...b, label, command } : b))
      : [...buttonConfig.buttons, { id: genId(), label, command, terminator, sort: buttonConfig.buttons.length }];

    handleSerialButtonConfigChange("buttons", stableSort(nextButtons) );
    setEditorOpen(null);
  };

  /** simple reordering: alphabetical by sort, then label */
  const sortedButtons = useMemo(() => buttonConfig.buttons, [buttonConfig.buttons]);

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
              size="XS"
              theme="primary"
              LeadingIcon={buttonConfig.hideSerialSettings ? LuEye : LuEyeOff}
              text={buttonConfig.hideSerialSettings ? "Show Settings" : "Hide Settings"}
              onClick={() => handleSerialButtonConfigChange("hideSerialSettings", !buttonConfig.hideSerialSettings )}
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
                  value={buttonConfig.baudRate}
                  onChange={(e) => handleSerialButtonConfigChange("baudRate", e.target.value)}
                />

                <SelectMenuBasic
                  label="Data Bits"
                  options={[
                    { label: "8", value: "8" },
                    { label: "7", value: "7" },
                  ]}
                  value={buttonConfig.dataBits}
                  onChange={(e) => handleSerialButtonConfigChange("dataBits", e.target.value)}
                />

                <SelectMenuBasic
                  label="Stop Bits"
                  options={[
                    { label: "1", value: "1" },
                    { label: "1.5", value: "1.5" },
                    { label: "2", value: "2" },
                  ]}
                  value={buttonConfig.stopBits}
                  onChange={(e) => handleSerialButtonConfigChange("stopBits", e.target.value)}
                />

                <SelectMenuBasic
                  label="Parity"
                  options={[
                    { label: "None", value: "none" },
                    { label: "Even", value: "even" },
                    { label: "Odd", value: "odd" },
                  ]}
                  value={buttonConfig.parity}
                  onChange={(e) => handleSerialButtonConfigChange("parity", e.target.value)}
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
                    onChange={(e) => handleSerialButtonConfigChange("terminator", {label: e.target.selectedOptions[0].text, value: e.target.value})}
                  />
                  <div className="text-xs text-white opacity-70 mt-0 ml-2">
                    When sent, the selected line ending ({buttonConfig.terminator.label}) will be appended.
                  </div>
                </div>
                <div>
                  <SelectMenuBasic
                    className="mb-1"
                    label="Terminal Mode"
                    options={[
                      { label: "Raw Mode", value: "raw" },
                      { label: "Line Mode", value: "line" },
                    ]}
                    value={buttonConfig.lineMode ? "line" : "raw"}
                    onChange={(e) => {
                      handleSerialButtonConfigChange("lineMode", e.target.value === "line")
                      setTerminalLineMode(e.target.value === "line");
                    }}
                  />
                  <div className="text-xs text-white opacity-70 mt-0 ml-2">
                    {buttonConfig.lineMode
                      ? "In Line Mode, input is sent when you press Enter in the input field."
                      : "In Raw Mode, input is sent immediately as you type in the console."}
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
                      handleSerialButtonConfigChange("enableEcho", e.target.checked);
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

