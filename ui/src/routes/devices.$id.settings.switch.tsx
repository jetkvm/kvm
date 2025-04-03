import { useState, useEffect, Fragment, useCallback } from "react";

import { Button } from "@/components/Button";
import TextArea from "@/components/TextArea";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { MdOutlineDelete, MdOutlineNorth, MdOutlinePlusOne, MdOutlineSouth } from "react-icons/md";
import { SettingsPageHeader } from "@components/SettingsPageheader";

import notifications from "../notifications";
import { SelectMenuBasic } from "../components/SelectMenuBasic";

import { SettingsItem } from "./devices.$id.settings";
import { Checkbox } from "@/components/Checkbox";
import { GridCard } from "@components/Card";
import InputField, { FieldError, InputFieldWithLabel } from "@components/InputField";
import FieldLabel from "@components/FieldLabel";
import { v4 as uuidv4 } from 'uuid'



/**
 * Switch channel command definition (what should be sent if channel is selected)
 */
export interface SwitchChannelCommands {
  // Remote address
  address: string;
  // Protocol to send data in
  protocol: "tcp" | "udp" | "http" | "https";
  // Command format
  format: "hex" | "base64" | "ascii" | "http-raw";
  // Comma separated commands
  commands: string;
}

/**
 * Switch channel definition
 */
export interface SwitchChannel {
  name: string;
  id: string;
  commands: SwitchChannelCommands[];
}

export const CommandsTextHelp: Record<SwitchChannelCommands["format"], string> = {
  hex: "Provide comma separated list of commands, e.g. 0x24,0x68,0xA40A",
  base64: "Provide comma separated list of commands in base64, e.g. aGVsbG8=,d29ybGQ=",
  ascii: "Provide newline separated list of commands, e.g. hello\nworld",
  "http-raw": "Provide raw HTTP request, e.g. GET / HTTP/1.1",
}

export const GetCompatibleCommands = (protocol: SwitchChannelCommands["protocol"]) => {
  if (protocol == "http" || protocol == "https") {
    return ["http-raw"];
  } else {
    return ["hex", "base64", "ascii"];
  }
}

export const CommandsTextPlaceholder: Record<SwitchChannelCommands["format"], string> = {
  hex: "0x24,0x68,0xA4",
  base64: "aGVsbG8=,d29ybGQ=",
  ascii: "hello\nworld",
  "http-raw": `GET /images HTTP/1.1
Host: example.com
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.114 Safari/537.36
Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9
Accept-Encoding: gzip, deflate
Connection: close`
}

export const GenerateSwitchChannelId = () => {
  return uuidv4();
}

export default function SettingsSwitchRoute() {
  const [send] = useJsonRpc();
  const [kvmSwitchEnabled, setKvmSwitchEnabled] = useState<boolean | null>(null);
  const [switchChannels, setSwitchChannels] = useState<SwitchChannel[]>([]);

  const updateSwitchChannelData = (index: number, data: SwitchChannel) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels[index] = data;
    setSwitchChannels(newSwitchChannels);
  };

  const updateSwitchChannelCommands = (index: number, channelIndex: number, data: SwitchChannelCommands) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels[index].commands[channelIndex] = data;
    setSwitchChannels(newSwitchChannels);
  };

  const removeChannelById = (index: number) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels.splice(index, 1);
    setSwitchChannels(newSwitchChannels);
  };

  const removeCommandById = (index: number, channelIndex: number) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels[index].commands.splice(channelIndex, 1);
    setSwitchChannels(newSwitchChannels);
  };

  const addChannel = (afterIndex: number) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels.splice(afterIndex + 1, 0, {
      name: "",
      id: GenerateSwitchChannelId(),
      commands: [
        {
          address: "",
          protocol: "tcp",
          format: "hex",
          commands: "",
        },
      ],
    });

    setSwitchChannels(newSwitchChannels);
  };

  const addCommand = (channelIndex: number, afterIndex: number) => {
    const newSwitchChannels = [...switchChannels];
    newSwitchChannels[channelIndex].commands.splice(afterIndex + 1, 0, {
      address: "",
      protocol: "tcp",
      format: "hex",
      commands: "",
    });
    setSwitchChannels(newSwitchChannels);
  }

  const moveChannel = (fromIndex: number, moveUp: boolean) => {
    const newSwitchChannels = [...switchChannels];
    const channel = newSwitchChannels[fromIndex];
    newSwitchChannels.splice(fromIndex, 1);
    newSwitchChannels.splice(fromIndex + (moveUp ? -1 : 1), 0, channel);
    setSwitchChannels(newSwitchChannels);
  }

  const moveCommand = (channelIndex: number, fromIndex: number, moveUp: boolean) => {
    const newSwitchChannels = [...switchChannels];
    const command = newSwitchChannels[channelIndex].commands[fromIndex];
    newSwitchChannels[channelIndex].commands.splice(fromIndex, 1);
    newSwitchChannels[channelIndex].commands.splice(fromIndex + (moveUp ? -1 : 1), 0, command);
    setSwitchChannels(newSwitchChannels);
  }

  useEffect(() => {
    send("getKvmSwitchEnabled", {}, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get KVM switch state: ${resp.error.data || "Unknown error"}`);
        return
      }
      const enabled = Boolean(resp.result);
      setKvmSwitchEnabled(enabled);

      if (enabled) {
        send("getKvmSwitchChannels", {}, resp => {
          if ("error" in resp) {
            notifications.error(`Failed to get switch channels: ${resp.error.data || "Unknown error"}`);
            return
          }
          setSwitchChannels(resp.result as SwitchChannel[]);
        })
      } else {
        setSwitchChannels([]);
      }

    });
  }, [send]);

  const setKvmSwitchEnabledHandler = (enabled: boolean) => {
    send("setKvmSwitchEnabled", { enabled: Boolean(enabled) }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set KVM switch state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      if (enabled) {
        notifications.success(`Enabled KVM Switch integration`);
      } else {
        notifications.success(`Disabled KVM Switch integration`);
      }
      setKvmSwitchEnabled(enabled);
    });
  };

  const getErrorsForChannelCommand = useCallback((index: number, channelIndex: number) => {
    const channel = switchChannels[index];
    const command = channel.commands[channelIndex];

    let errors: string[] = [];

    // Check name
    const nameValue = channel.name.trim();
    if (nameValue.length === 0) {
      errors.push("Name cannot be empty");
    }

    // Check address
    if (command.protocol !== "http" && command.protocol !== "https") {
      const addressValue = command.address.trim();
      // Check that it is in form host:port
      const addressParts = addressValue.split(":");
      if (addressValue.length === 0) {
        errors.push("Address cannot be empty");
      } else if (addressParts.length !== 2) {
        errors.push("Address must be in form host:port");
      } else if (addressParts[0].length === 0) {
        errors.push("Address host cannot be empty");
      } else if (addressParts[1].length === 0) {
        errors.push("Address port cannot be empty");
      } else if (!/^\d+$/.test(addressParts[1])) {
        errors.push("Address port must be a number");
      }
    }

    // Check that commands map to the format
    if (command.format === "hex") {
      const messages = command.commands.split(",").map(x => x.trim());
      if (messages.length === 0) {
        errors.push("Commands cannot be empty");
      } else if (messages.some(x => !/^\s*0x[0-9a-fA-F]+\s*$/.test(x))) {
        errors.push("Commands must be in hex format (0x123020392193)");
      }
    }
    if (command.format === "base64") {
      const messages = command.commands.split(",").map(x => x.trim());
      if (messages.length === 0) {
        errors.push("Commands cannot be empty");
      }
      let anyBase64Failed = false;
      if ("atob" in window) {
        for (const message of messages) {
          try {
            atob(message);
          } catch (e) {
            anyBase64Failed = true;
          }
        }
      }
      if (anyBase64Failed) {
        errors.push("Commands must be in base64 format");
      }
    }
    if (command.format === "http-raw") {
      // Check that main components of HTTP request are in the command field
      const commandValue = command.commands.trim();
      if (commandValue.length === 0) {
        errors.push("HTTP request cannot be empty");
      } else {
        // Check that Host header is present
        if (!commandValue.includes("Host:")) {
          errors.push("HTTP request must contain Host header");
        }
      }
    }
    return errors;
  }, [switchChannels]);


  useEffect(() => {
    if (!kvmSwitchEnabled) {
      return
    }

    // Iterate over getErrorsForChannelCommand
    var anyErrorsFound = false;
    for (const index in switchChannels) {
      if (anyErrorsFound) {
        break;
      }
      for (const channelIndex in switchChannels[index].commands) {
        const errors = getErrorsForChannelCommand(Number(index), Number(channelIndex));
        if (errors.length > 0) {
          anyErrorsFound = true;
          break;
        }
      }
    }

    if (anyErrorsFound) {
      return;
    }

    const payload = {
      config: {
        channels: switchChannels
      }
    };

    send("setKvmSwitchChannels", payload, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set switch channels: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
    });
  }, [kvmSwitchEnabled, switchChannels, getErrorsForChannelCommand]);

  return (
    <div className="space-y-3">
      <div className="space-y-4">
        <SettingsPageHeader
          title="KVM Switch"
          description="Configure remote KVM switch"
        />

        <div className="space-y-4">
          <div className="space-y-4">
            <SettingsItem
              title="Enable remote KVM Switch"
              description="Enable ability to switch devices using external KVM Switch"
            >
              <Checkbox
                checked={kvmSwitchEnabled ?? false}
                disabled={kvmSwitchEnabled === null}
                onChange={e => setKvmSwitchEnabledHandler(e.target.checked)}
              />
            </SettingsItem>

            {!!kvmSwitchEnabled && switchChannels.length > 0 && (
              <div className="space-y-4 flex flex-col">
                {switchChannels.map((option, index) => (
                  <GridCard key={index}>
                    <div className="space-y-4 p-4 py-3">
                      <div className="flex items-center justify-between">
                        <FieldLabel label={"Channel #" + (index + 1)} />
                        <div className="flex items-center space-x-1">
                          {switchChannels.length > 1 && (
                            <Fragment>
                              <Button
                                size="XS"
                                theme="light"
                                text=""
                                LeadingIcon={MdOutlineNorth}
                                onClick={() => {
                                  moveChannel(index, true);
                                }}
                              />
                              <Button
                                size="XS"
                                theme="light"
                                text=""
                                LeadingIcon={MdOutlineSouth}
                                onClick={() => {
                                  moveChannel(index, false);
                                }}
                              />
                            </Fragment>
                          )}
                          <Button
                            size="XS"
                            theme="light"
                            text=""
                            LeadingIcon={MdOutlinePlusOne}
                            onClick={() => {
                              addChannel(index);
                            }}
                          />
                          <Button
                            size="XS"
                            theme="danger"
                            text=""
                            LeadingIcon={MdOutlineDelete}
                            onClick={() => {
                              // Remove channel by index
                              removeChannelById(index);
                            }}
                          />
                        </div>
                      </div>
                      <InputFieldWithLabel
                        label="Channel Name"
                        type="text"
                        placeholder="Channel name for UI"
                        value={option.name}
                        onChange={e => updateSwitchChannelData(index, { ...option, name: e.target.value })}
                      />
                      {option.commands.map((command, commandIndex) => (
                        <div key={commandIndex} className="space-y-4">
                          <div className="flex items-center space-x-2">
                            <div className="flex-1">
                              <FieldLabel label={"Command #" + (commandIndex + 1)} />
                            </div>
                            <div className="flex items-center space-x-1">
                              {option.commands.length > 1 && (
                                <Fragment>
                                  <Button
                                    size="XS"
                                    theme="light"
                                    text=""
                                    LeadingIcon={MdOutlineNorth}
                                    onClick={() => {
                                      moveCommand(index, commandIndex, true);
                                    }}
                                  />
                                  <Button
                                    size="XS"
                                    theme="light"
                                    text=""
                                    LeadingIcon={MdOutlineSouth}
                                    onClick={() => {
                                      moveCommand(index, commandIndex, false);
                                    }}
                                  />

                                </Fragment>
                              )}
                              <Button
                                size="XS"
                                theme="light"
                                text=""
                                LeadingIcon={MdOutlinePlusOne}
                                onClick={() => {
                                  addCommand(index, commandIndex);
                                }}
                              />
                              {option.commands.length > 1 && (
                                <Button
                                  size="XS"
                                  theme="danger"
                                  text=""
                                  LeadingIcon={MdOutlineDelete}
                                  onClick={() => {
                                    removeCommandById(index, commandIndex);
                                  }}
                                />
                              )}
                            </div>

                          </div>
                          <div className="flex items-center space-x-2">
                            <div className="w-full space-y-1">
                              <FieldLabel label="Address" id={`address-${index}-${commandIndex}`} as="span" />
                              <InputField
                                id={`address-${index}-${commandIndex}`}
                                type="text"
                                placeholder={command.protocol == "http" || command.protocol == "https" ? "DISABLED FOR HTTP(s)" : "127.0.0.1"}
                                disabled={command.protocol == "http" || command.protocol == "https"}
                                value={command.protocol == "http" || command.protocol == "https" ? "" : command.address}
                                onChange={e => updateSwitchChannelCommands(index, commandIndex, { ...command, address: e.target.value })}
                              />
                            </div>
                            <SelectMenuBasic
                              label="Protocol"
                              value={command.protocol}
                              fullWidth
                              onChange={e => updateSwitchChannelCommands(
                                index,
                                commandIndex,
                                {
                                  ...command,
                                  protocol: e.target.value as SwitchChannelCommands["protocol"],
                                  format: GetCompatibleCommands(e.target.value as SwitchChannelCommands["protocol"])[0] as SwitchChannelCommands["format"],
                                  commands: ""
                                })}
                              options={[
                                { label: "TCP", value: "tcp" },
                                { label: "UDP", value: "udp" },
                                { label: "HTTP", value: "http" },
                                { label: "HTTPS", value: "https" },
                              ]}
                            />
                            <SelectMenuBasic
                              label="Format"
                              value={command.format}
                              fullWidth
                              onChange={e => updateSwitchChannelCommands(
                                index,
                                commandIndex,
                                {
                                  ...command,
                                  format: e.target.value as SwitchChannelCommands["format"], commands: ""
                                })}
                              options={[
                                { label: "HEX", value: "hex" },
                                { label: "Base64", value: "base64" },
                                { label: "ASCII", value: "ascii" },
                                { label: "HTTP Raw", value: "http-raw" },
                              ].filter(f => GetCompatibleCommands(command.protocol).includes(f.value))}
                            />
                          </div>
                          <div className="w-full space-y-1">
                            <FieldLabel label="Command" id={`command-${index}-${commandIndex}`} as="span" description={CommandsTextHelp[command.format]} />
                            <TextArea
                              id={`command-${index}-${commandIndex}`}
                              placeholder={CommandsTextPlaceholder[command.format]}
                              value={command.commands}
                              onChange={e => updateSwitchChannelCommands(index, commandIndex, { ...command, commands: e.target.value })}
                            />
                          </div>
                          {getErrorsForChannelCommand(index, commandIndex).length > 0 && (
                            <div className="w-full space-y-1">
                              <FieldError error={getErrorsForChannelCommand(index, commandIndex).join(", ")} />
                            </div>
                          )}
                          {(commandIndex < option.commands.length - 1) && (<div className="h-px bg-gray-200 dark:bg-gray-700" />)}
                        </div>
                      ))}
                    </div>

                  </GridCard>
                ))}
              </div>
            )}

            {!!kvmSwitchEnabled && switchChannels.length == 0 && (
              <div className="max-w-3xl">
                <Button
                  size="LG"
                  theme="primary"
                  text="Add Channel"
                  onClick={() => {
                    addChannel(0);
                  }}
                />
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
