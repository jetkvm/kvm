import SidebarHeader from "@components/SidebarHeader";
import {
  useLocalAuthModalStore,
  useSettingsStore,
  useUiStore,
  useUpdateStore,
} from "@/hooks/stores";
import { Checkbox } from "@components/Checkbox";
import { Button, LinkButton } from "@components/Button";
import { TextAreaWithLabel } from "@components/TextArea";
import { SectionHeader } from "@components/SectionHeader";
import { GridCard } from "@components/Card";
import { CheckCircleIcon } from "@heroicons/react/20/solid";
import { cx } from "@/cva.config";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { isOnDevice } from "@/main";
import PointingFinger from "@/assets/pointing-finger.svg";
import MouseIcon from "@/assets/mouse-icon.svg";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "../SelectMenuBasic";
import { SystemVersionInfo } from "@components/UpdateDialog";
import notifications from "@/notifications";
import api from "../../api";
import LocalAuthPasswordDialog from "@/components/LocalAuthPasswordDialog";
import { LocalDevice } from "@routes/devices.$id";
import { useRevalidator } from "react-router-dom";
import { ShieldCheckIcon } from "@heroicons/react/20/solid";

export function SettingsItem({
  title,
  description,
  children,
  className,
}: {
  title: string;
  description: string | React.ReactNode;
  children?: React.ReactNode;
  className?: string;
  name?: string;
}) {
  return (
    <label className={cx("flex items-center justify-between gap-x-4 rounded", className)}>
      <div className="space-y-0.5">
        <h3 className="text-base font-semibold text-black dark:text-white">{title}</h3>
        <p className="text-sm text-slate-700 dark:text-slate-300">{description}</p>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}

const defaultEdid =
  "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b";
const edids = [
  {
    value: defaultEdid,
    label: "JetKVM Default",
  },
  {
    value:
      "00FFFFFFFFFFFF00047265058A3F6101101E0104A53420783FC125A8554EA0260D5054BFEF80714F8140818081C081008B009500B300283C80A070B023403020360006442100001A000000FD00304C575716010A202020202020000000FC0042323436574C0A202020202020000000FF0054384E4545303033383532320A01F802031CF14F90020304050607011112131415161F2309070783010000011D8018711C1620582C250006442100009E011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E9600064421000018C344806E70B028401720A80406442100001E00000000000000000000000000000000000000000000000000000096",
    label: "Acer B246WL, 1920x1200",
  },
  {
    value:
      "00FFFFFFFFFFFF0006B3872401010101021F010380342078EA6DB5A7564EA0250D5054BF6F00714F8180814081C0A9409500B300D1C0283C80A070B023403020360006442100001A000000FD00314B1E5F19000A202020202020000000FC00504132343851560A2020202020000000FF004D314C4D51533035323135370A014D02032AF14B900504030201111213141F230907078301000065030C001000681A00000101314BE6E2006A023A801871382D40582C450006442100001ECD5F80B072B0374088D0360006442100001C011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E960006442100001800000000000000000000000000DC",
    label: "ASUS PA248QV, 1920x1200",
  },
  {
    value:
      "00FFFFFFFFFFFF0010AC132045393639201E0103803C22782ACD25A3574B9F270D5054A54B00714F8180A9C0D1C00101010101010101023A801871382D40582C450056502100001E000000FF00335335475132330A2020202020000000FC0044454C4C204432373231480A20000000FD00384C1E5311000A202020202020018102031AB14F90050403020716010611121513141F65030C001000023A801871382D40582C450056502100001E011D8018711C1620582C250056502100009E011D007251D01E206E28550056502100001E8C0AD08A20E02D10103E960056502100001800000000000000000000000000000000000000000000000000000000004F",
    label: "DELL D2721H, 1920x1080",
  },
];

export default function SettingsSidebar() {
  const setSidebarView = useUiStore(state => state.setSidebarView);
  const settings = useSettingsStore();
  const [send] = useJsonRpc();
  const [streamQuality, setStreamQuality] = useState("1");
  const [autoUpdate, setAutoUpdate] = useState(true);
  const [devChannel, setDevChannel] = useState(false);
  const [jiggler, setJiggler] = useState(false);
  const [edid, setEdid] = useState<string | null>(null);
  const [customEdidValue, setCustomEdidValue] = useState<string | null>(null);

  const [isAdopted, setAdopted] = useState(false);
  const [deviceId, setDeviceId] = useState<string | null>(null);

  const [sshKey, setSSHKey] = useState<string>("");
  const [localDevice, setLocalDevice] = useState<LocalDevice | null>(null);

  const sidebarRef = useRef<HTMLDivElement>(null);

  const hideCursor = useSettingsStore(state => state.isCursorHidden);
  const setHideCursor = useSettingsStore(state => state.setCursorVisibility);
  const setDeveloperMode = useSettingsStore(state => state.setDeveloperMode);

  const [currentVersions, setCurrentVersions] = useState<{
    appVersion: string;
    systemVersion: string;
  } | null>(null);

  const [usbEmulationEnabled, setUsbEmulationEnabled] = useState(false);
  const getUsbEmulationState = useCallback(() => {
    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });
  }, [send]);

  const handleUsbEmulationToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEmulationState", { enabled: enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB emulation: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setUsbEmulationEnabled(enabled);
        getUsbEmulationState();
      });
    },
    [getUsbEmulationState, send],
  );

  const getCloudState = useCallback(() => {
    send("getCloudState", {}, resp => {
      if ("error" in resp) return console.error(resp.error);
      const cloudState = resp.result as { connected: boolean };
      setAdopted(cloudState.connected);
    });
  }, [send]);

  const deregisterDevice = async () => {
    send("deregisterDevice", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to de-register device: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      getCloudState();
      return;
    });
  };

  const handleStreamQualityChange = (factor: string) => {
    send("setStreamQualityFactor", { factor: Number(factor) }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set stream quality: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setStreamQuality(factor);
    });
  };

  const handleAutoUpdateChange = (enabled: boolean) => {
    send("setAutoUpdateState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set auto-update: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setAutoUpdate(enabled);
    });
  };

  const handleDevChannelChange = (enabled: boolean) => {
    send("setDevChannelState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set dev channel state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setDevChannel(enabled);
    });
  };

  const handleJigglerChange = (enabled: boolean) => {
    send("setJigglerState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set jiggler state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setJiggler(enabled);
    });
  };

  const handleEDIDChange = (newEdid: string) => {
    send("setEDID", { edid: newEdid }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set EDID: ${resp.error.data || "Unknown error"}`);
        return;
      }

      // Update the EDID value in the UI
      setEdid(newEdid);
    });
  };

  const handleSSHKeyChange = (newKey: string) => {
    setSSHKey(newKey);
  };

  const handleDevModeChange = useCallback(
    (developerMode: boolean) => {
      send("setDevModeState", { enabled: developerMode }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set dev mode: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setDeveloperMode(developerMode);
        setTimeout(() => {
          sidebarRef.current?.scrollTo({ top: 5000, behavior: "smooth" });
        }, 0);
      });
    },
    [send, setDeveloperMode],
  );

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update SSH key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("SSH key updated successfully");
    });
  }, [send, sshKey]);

  const { setIsUpdateDialogOpen, setModalView, otaState } = useUpdateStore();
  const handleCheckForUpdates = () => {
    if (otaState.updating) {
      setModalView("updating");
      setIsUpdateDialogOpen(true);
    } else {
      setModalView("loading");
      setIsUpdateDialogOpen(true);
    }
  };

  useEffect(() => {
    getCloudState();

    send("getDeviceID", {}, async resp => {
      if ("error" in resp) return console.error(resp.error);
      setDeviceId(resp.result as string);
    });

    send("getJigglerState", {}, resp => {
      if ("error" in resp) return;
      setJiggler(resp.result as boolean);
    });

    send("getAutoUpdateState", {}, resp => {
      if ("error" in resp) return;
      setAutoUpdate(resp.result as boolean);
    });

    send("getDevChannelState", {}, resp => {
      if ("error" in resp) return;
      setDevChannel(resp.result as boolean);
    });

    send("getStreamQualityFactor", {}, resp => {
      if ("error" in resp) return;
      setStreamQuality(String(resp.result));
    });

    send("getEDID", {}, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get EDID: ${resp.error.data || "Unknown error"}`);
        return;
      }

      const receivedEdid = resp.result as string;

      const matchingEdid = edids.find(
        x => x.value.toLowerCase() === receivedEdid.toLowerCase(),
      );

      if (matchingEdid) {
        // EDID is stored in uppercase in the UI
        setEdid(matchingEdid.value.toUpperCase());
        // Reset custom EDID value
        setCustomEdidValue(null);
      } else {
        setEdid("custom");
        setCustomEdidValue(receivedEdid);
      }
    });

    send("getDevModeState", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as { enabled: boolean };
      setDeveloperMode(result.enabled);
    });

    send("getSSHKeyState", {}, resp => {
      if ("error" in resp) return;
      setSSHKey(resp.result as string);
    });

    send("getUpdateStatus", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as SystemVersionInfo;
      setCurrentVersions({
        appVersion: result.local.appVersion,
        systemVersion: result.local.systemVersion,
      });
    });

    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });
  }, [getCloudState, send, setDeveloperMode, setHideCursor, setJiggler]);

  const getDevice = useCallback(async () => {
    try {
      const status = await api
        .GET(`${import.meta.env.VITE_SIGNAL_API}/device`)
        .then(res => res.json() as Promise<LocalDevice>);
      setLocalDevice(status);
    } catch (error) {
      notifications.error("Failed to get authentication status");
    }
  }, []);

  const { setModalView: setLocalAuthModalView } = useLocalAuthModalStore();
  const [isLocalAuthDialogOpen, setIsLocalAuthDialogOpen] = useState(false);

  useEffect(() => {
    if (isOnDevice) getDevice();
  }, [getDevice]);

  useEffect(() => {
    if (!isOnDevice) return;
    // Refresh device status when the local auth dialog is closed
    if (!isLocalAuthDialogOpen) {
      getDevice();
    }
  }, [getDevice, isLocalAuthDialogOpen]);

  const revalidator = useRevalidator();

  const [currentTheme, setCurrentTheme] = useState(() => {
    return localStorage.theme || "system";
  });

  const handleThemeChange = useCallback((value: string) => {
    const root = document.documentElement;

    if (value === "system") {
      localStorage.removeItem("theme");
      // Check system preference
      const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
      root.classList.remove("light", "dark");
      root.classList.add(systemTheme);
    } else {
      localStorage.theme = value;
      root.classList.remove("light", "dark");
      root.classList.add(value);
    }
  }, []);

  const handleResetConfig = useCallback(() => {
    send("resetConfig", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to reset configuration: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Configuration reset to default successfully");
    });
  }, [send]);

  return (
    <div
      className="grid h-full shadow-sm grid-rows-headerBody"
      // Prevent the keyboard entries from propagating to the document where they are listened for and sent to the KVM
      onKeyDown={e => e.stopPropagation()}
      onKeyUp={e => e.stopPropagation()}
    >
      <SidebarHeader title="Settings" setSidebarView={setSidebarView} />
      <div
        className="h-full px-4 py-2 space-y-4 overflow-y-scroll bg-white dark:bg-slate-900"
        ref={sidebarRef}
      >
        <div className="space-y-4">
          <div className="flex items-center justify-between mt-2 gap-x-2">
            <SettingsItem
              title="Check for Updates"
              description={
                currentVersions ? (
                  <>
                    App: {currentVersions.appVersion}
                    <br />
                    System: {currentVersions.systemVersion}
                  </>
                ) : (
                  "Loading current versions..."
                )
              }
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text="Check for Updates"
                onClick={handleCheckForUpdates}
              />
            </div>
          </div>
          <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
          <SectionHeader
            title="Mouse"
            description="Customize mouse behavior and interaction settings"
          />

          <div className="space-y-4">
            <SettingsItem
              title="Hide Cursor"
              description="Hide the cursor when sending mouse movements"
            >
              <Checkbox
                checked={hideCursor}
                onChange={e => {
                  setHideCursor(e.target.checked);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title="Jiggler"
              description="Simulate movement of a computer mouse. Prevents sleep mode, standby mode or the screensaver from activating"
            >
              <Checkbox
                checked={jiggler}
                onChange={e => {
                  handleJigglerChange(e.target.checked);
                }}
              />
            </SettingsItem>
            <div className="space-y-4">
              <SettingsItem title="Modes" description="Choose the mouse input mode" />
              <div className="flex items-center gap-4">
                <button
                  className="block group grow"
                  onClick={() => console.log("Absolute mouse mode clicked")}
                >
                  <GridCard>
                    <div className="flex items-center px-4 py-3 group gap-x-4">
                      <img
                        className="w-6 shrink-0"
                        src={PointingFinger}
                        alt="Finger touching a screen"
                      />
                      <div className="flex items-center justify-between grow">
                        <div className="text-left">
                          <h3 className="text-sm font-semibold text-black dark:text-white">
                            Absolute
                          </h3>
                          <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                            Most convenient
                          </p>
                        </div>
                        <CheckCircleIcon className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                      </div>
                    </div>
                  </GridCard>
                </button>
                <button
                  className="block opacity-50 cursor-not-allowed group grow"
                  disabled
                >
                  <GridCard>
                    <div className="flex items-center px-4 py-3 gap-x-4">
                      <img className="w-6 shrink-0" src={MouseIcon} alt="Mouse icon" />
                      <div className="flex items-center justify-between grow">
                        <div className="text-left">
                          <h3 className="text-sm font-semibold text-black dark:text-white">
                            Relative
                          </h3>
                          <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                            Coming soon
                          </p>
                        </div>
                      </div>
                    </div>
                  </GridCard>
                </button>
              </div>
            </div>
          </div>
        </div>
        <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
        <div className="pb-2 space-y-4">
          <SectionHeader
            title="Video"
            description="Configure display settings and EDID for optimal compatibility"
          />
          <div className="space-y-4">
            <SettingsItem
              title="Stream Quality"
              description="Adjust the quality of the video stream"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={streamQuality}
                options={[
                  { value: "1", label: "High" },
                  { value: "0.5", label: "Medium" },
                  { value: "0.1", label: "Low" },
                ]}
                onChange={e => handleStreamQualityChange(e.target.value)}
              />
            </SettingsItem>
            <SettingsItem
              title="EDID"
              description="Adjust the EDID settings for the display"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                fullWidth
                value={customEdidValue ? "custom" : edid || "asd"}
                onChange={e => {
                  if (e.target.value === "custom") {
                    setEdid("custom");
                    setCustomEdidValue("");
                  } else {
                    handleEDIDChange(e.target.value as string);
                  }
                }}
                options={[...edids, { value: "custom", label: "Custom" }]}
              />
            </SettingsItem>
            {customEdidValue !== null && (
              <>
                <SettingsItem
                  title="Custom EDID"
                  description="EDID details video mode compatibility. Default settings works in most cases, but unique UEFI/BIOS might need adjustments."
                />
                <TextAreaWithLabel
                  label="EDID File"
                  placeholder="00F..."
                  rows={3}
                  value={customEdidValue}
                  onChange={e => setCustomEdidValue(e.target.value)}
                />
                <div className="flex justify-start gap-x-2">
                  <Button
                    size="MD"
                    theme="primary"
                    text="Set Custom EDID"
                    onClick={() => handleEDIDChange(customEdidValue)}
                  />
                  <Button
                    size="MD"
                    theme="light"
                    text="Restore to default"
                    onClick={() => {
                      setCustomEdidValue(null);
                      handleEDIDChange(defaultEdid);
                    }}
                  />
                </div>
              </>
            )}
          </div>
        </div>
        {isOnDevice && (
          <>
            <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <div className="pb-4 space-y-4">
              <SectionHeader
                title="JetKVM Cloud"
                description="Connect your device to the cloud for secure remote access and management"
              />

              <GridCard>
                <div className="flex items-start p-4 gap-x-4">
                  <ShieldCheckIcon className="w-8 h-8 mt-1 text-blue-600 shrink-0 dark:text-blue-500" />
                  <div className="space-y-3">
                    <div className="space-y-2">
                      <h3 className="text-base font-bold text-slate-900 dark:text-white">
                        Cloud Security
                      </h3>
                      <div>
                        <ul className="space-y-1 text-xs text-slate-700 dark:text-slate-300">
                          <li>• End-to-end encryption using WebRTC (DTLS and SRTP)</li>
                          <li>• Zero Trust security model</li>
                          <li>• OIDC (OpenID Connect) authentication</li>
                          <li>• All streams encrypted in transit</li>
                        </ul>
                      </div>

                      <div className="text-xs text-slate-700 dark:text-slate-300">
                        All cloud components are open-source and available on{" "}
                        <a
                          href="https://github.com/jetkvm"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="font-medium text-blue-600 hover:text-blue-800 dark:text-blue-500 dark:hover:text-blue-400"
                        >
                          GitHub
                        </a>
                        .
                      </div>
                    </div>
                    <hr className="block w-full dark:border-slate-600" />

                    <div>
                      <LinkButton
                        to="https://jetkvm.com/docs/networking/remote-access"
                        size="SM"
                        theme="light"
                        text="Learn about our cloud security"
                      />
                    </div>
                  </div>
                </div>
              </GridCard>

              {!isAdopted ? (
                <div>
                  <LinkButton
                    to={
                      import.meta.env.VITE_CLOUD_APP +
                      "/signup?deviceId=" +
                      deviceId +
                      `&returnTo=${location.href}adopt`
                    }
                    size="MD"
                    theme="primary"
                    text="Adopt KVM to Cloud account"
                  />
                </div>
              ) : (
                <div>
                  <div className="space-y-2">
                    <p className="text-sm text-slate-600 dark:text-slate-300">
                      Your device is adopted to JetKVM Cloud
                    </p>
                    <div>
                      <Button
                        size="MD"
                        theme="light"
                        text="De-register from Cloud"
                        className="text-red-600"
                        onClick={() => {
                          if (deviceId) {
                            if (
                              window.confirm(
                                "Are you sure you want to de-register this device?",
                              )
                            ) {
                              deregisterDevice();
                            }
                          } else {
                            notifications.error("No device ID available");
                          }
                        }}
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>
          </>
        )}
        <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
        {isOnDevice ? (
          <>
            <div className="pb-2 space-y-4">
              <SectionHeader
                title="Local Access"
                description="Manage the mode of local access to the device"
              />

              <div className="space-y-4">
                <SettingsItem
                  title="Authentication Mode"
                  description={`Current mode: ${localDevice?.authMode === "password" ? "Password protected" : "No password"}`}
                >
                  {localDevice?.authMode === "password" ? (
                    <Button
                      size="SM"
                      theme="light"
                      text="Disable Protection"
                      onClick={() => {
                        setLocalAuthModalView("deletePassword");
                        setIsLocalAuthDialogOpen(true);
                      }}
                    />
                  ) : (
                    <Button
                      size="SM"
                      theme="light"
                      text="Enable Password"
                      onClick={() => {
                        setLocalAuthModalView("createPassword");
                        setIsLocalAuthDialogOpen(true);
                      }}
                    />
                  )}
                </SettingsItem>

                {localDevice?.authMode === "password" && (
                  <SettingsItem
                    title="Change Password"
                    description="Update your device access password"
                  >
                    <Button
                      size="SM"
                      theme="light"
                      text="Change Password"
                      onClick={() => {
                        setLocalAuthModalView("updatePassword");
                        setIsLocalAuthDialogOpen(true);
                      }}
                    />
                  </SettingsItem>
                )}
              </div>
            </div>
            <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
          </>
        ) : null}
        <div className="pb-2 space-y-4">
          <SectionHeader
            title="Updates"
            description="Manage software updates and version information"
          />

          <div className="space-y-4">
            <SettingsItem
              title="Auto Update"
              description="Automatically update the device to the latest version"
            >
              <Checkbox
                checked={autoUpdate}
                onChange={e => {
                  handleAutoUpdateChange(e.target.checked);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title="Dev Channel Updates"
              description="Receive early updates from the development channel"
            >
              <Checkbox
                checked={devChannel}
                onChange={e => {
                  handleDevChannelChange(e.target.checked);
                }}
              />
            </SettingsItem>
          </div>
        </div>
        <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

        <SectionHeader
          title="Appearance"
          description="Customize the look and feel of the application"
        />
        <SettingsItem title="Theme" description="Choose your preferred color theme">
          <SelectMenuBasic
            size="SM"
            label=""
            value={currentTheme}
            options={[
              { value: "system", label: "System" },
              { value: "light", label: "Light" },
              { value: "dark", label: "Dark" },
            ]}
            onChange={e => {
              setCurrentTheme(e.target.value);
              handleThemeChange(e.target.value);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title="Ctrl + Alt + Del Button"
          description="Display Ctrl + Alt + Del button on the Action Bar"
        >
          <Checkbox
            checked={settings.actionBarCtrlAltDel}
            onChange={e => settings.setActionBarCtrlAltDel(e.target.checked)}
          />
        </SettingsItem>
        <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
        <div className="pb-2 space-y-4">
          <SectionHeader
            title="Advanced"
            description="Access additional settings for troubleshooting and customization"
          />

          <div className="pb-4 space-y-4">
            <SettingsItem
              title="Developer Mode"
              description="Enable advanced features for developers"
            >
              <Checkbox
                checked={settings.developerMode}
                onChange={e => handleDevModeChange(e.target.checked)}
              />
            </SettingsItem>

            {settings.developerMode && (
              <div className="space-y-4">
                <TextAreaWithLabel
                  label="SSH Public Key"
                  value={sshKey || ""}
                  rows={3}
                  onChange={e => handleSSHKeyChange(e.target.value)}
                  placeholder="Enter your SSH public key"
                />
                <p className="text-xs text-slate-600 dark:text-slate-400">
                  The default SSH user is <strong>root</strong>.
                </p>
                <div className="flex items-center gap-x-2">
                  <Button
                    size="SM"
                    theme="primary"
                    text="Update SSH Key"
                    onClick={handleUpdateSSHKey}
                  />
                </div>
              </div>
            )}
            <SettingsItem
              title="Troubleshooting Mode"
              description="Diagnostic tools and additional controls for troubleshooting and development purposes"
            >
              <Checkbox
                defaultChecked={settings.debugMode}
                onChange={e => {
                  settings.setDebugMode(e.target.checked);
                }}
              />
            </SettingsItem>

            {settings.debugMode && (
              <>
                <SettingsItem
                  title="USB Emulation"
                  description="Control the USB emulation state"
                >
                  <Button
                    size="SM"
                    theme="light"
                    text={
                      usbEmulationEnabled
                        ? "Disable USB Emulation"
                        : "Enable USB Emulation"
                    }
                    onClick={() => handleUsbEmulationToggle(!usbEmulationEnabled)}
                  />
                </SettingsItem>
              </>
            )}
            {settings.debugMode && (
              <SettingsItem
                title="Reset Configuration"
                description="Reset the configuration file to its default state. This will log you out of the device."
              >
                <Button
                  size="SM"
                  theme="light"
                  text="Reset Config"
                  onClick={() => {
                    handleResetConfig();
                    window.location.reload();
                  }}
                />
              </SettingsItem>
            )}
          </div>
        </div>
      </div>
      <LocalAuthPasswordDialog
        open={isLocalAuthDialogOpen}
        setOpen={x => {
          // Revalidate the current route to refresh the local device status and dependent UI components
          revalidator.revalidate();
          setIsLocalAuthDialogOpen(x);
        }}
      />
    </div>
  );
}
