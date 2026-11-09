import { useCallback, useEffect, useState } from "react";
import { Navigate } from "react-router";
import { useDebounceValue } from "usehooks-ts";

import { LuCopy, LuTriangleAlert } from "react-icons/lu";

import { Checkbox } from "@components/Checkbox";
import Card, { GridCard } from "@components/Card";
import { Button } from "@components/Button";
import { guestUpdateDescription, useGuestUpdateStart } from "@components/GuestUpdateStatusCard";
import OsServicesInstallerButton from "@components/OsServicesInstallerButton";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { useCopyToClipboard } from "@components/useCopyToClipBoard";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import {
  useOsServicesInstaller,
  useOsServicesInstallerProgress,
} from "@hooks/useOsServicesInstaller";
import { OsServicesSettings, useCapability, useDeviceStore, useVideoStore } from "@hooks/stores";
import { clipboardShortcut } from "@hooks/useGuestClipboard";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

const formatResolution = (value: string) => value.replace("x", " × ");

const resolutionOptions = ["1280x720", "1920x1080", "2560x1440", "3840x2160"].map(value => ({
  value,
  label: formatResolution(value),
}));

// The live mode joins the dropdown as its own selected entry, labelled as
// current. It has a value of its own so the listed entry for the same mode
// stays selectable as the preference.
const currentResolutionValue = "current";
function resolutionOptionsWith(current: string | null) {
  if (current === null) return resolutionOptions;
  return [
    ...resolutionOptions,
    {
      value: currentResolutionValue,
      label: m.os_services_resolution_current({
        resolution: formatResolution(current),
      }),
    },
  ];
}

// Only a device with OS Services has OS Services settings; on any other the
// page sends its visitor to the general settings.
export default function SettingsOsServicesRoute() {
  const osServices = useCapability("os_services");
  const reported = useDeviceStore(state => state.capabilities !== null);
  if (osServices) return <OsServicesSettingsPage />;
  if (reported) return <Navigate to="../general" replace />;
  return (
    <SettingsPageHeader title={m.os_services_title()} description={m.os_services_description()} />
  );
}

function OsServicesSettingsPage() {
  const { send } = useJsonRpc();
  const [settings, setSettings] = useState<OsServicesSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const displayModeOptions = [
    { value: "extend", label: m.os_services_display_mode_extend() },
    { value: "mirror", label: m.os_services_display_mode_mirror() },
  ];

  // An RPC reply and a settings announcement can cross. The announcement
  // carries the newer state (a change the reply would reflect is announced
  // too), so a reply is applied only if no announcement landed since the
  // request went out.
  const overtaken = useCallback((sequenceAtSend: number) => {
    return useVideoStore.getState().osServicesSettingsSequence !== sequenceAtSend;
  }, []);

  const load = useCallback(() => {
    setLoading(true);
    setLoadError(null);
    const sequenceAtSend = useVideoStore.getState().osServicesSettingsSequence;
    void send("getOsServicesSettings", {}, (response: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in response) {
        const message = m.os_services_load_failed({
          error: response.error.data || m.unknown_error(),
        });
        setLoadError(message);
        notifications.error(message);
        return;
      }
      if (overtaken(sequenceAtSend)) return;
      setSettings(response.result as OsServicesSettings);
    });
  }, [send, overtaken]);

  useEffect(() => load(), [load]);

  // Games and OS-side changes can move the live mode away from the requested
  // resolution. The video element reports the geometry it decodes and
  // videoInputState events the source, so the dropdown shows what is
  // streaming; choosing a listed value requests it again.
  const { videoSource, hdmiState, width, height, osServicesSettings, osServices, guestUpdate } =
    useVideoStore();
  const startUpdate = useGuestUpdateStart();
  const installer = useOsServicesInstaller();
  const installerProgress = useOsServicesInstallerProgress();
  // The sender exits to install the update, so until it is back the KVM
  // reports no sender and no package it can install. The phase still says
  // that the update runs.
  const runningUpdate =
    guestUpdate?.phase === "downloading" ||
    guestUpdate?.phase === "sending" ||
    guestUpdate?.phase === "installing"
      ? guestUpdate
      : null;
  const { copy } = useCopyToClipboard();
  // A connected sender that has not registered video cannot act on the
  // video setting: it has no capture yet (no Screen Recording grant, no
  // display), or the KVM has faulted video or blocked repeated handovers.
  const videoUnavailable =
    osServices.connected && (!osServices.videoRegistered || !!osServices.videoError);
  const liveResolution =
    videoSource === "guest" && hdmiState === "ready" && width > 0 && height > 0
      ? `${width}x${height}`
      : null;
  const mismatched =
    settings !== null &&
    settings.video_enabled &&
    liveResolution !== null &&
    liveResolution !== settings.resolution;
  // A freshly requested mode takes a second or two to reach the stream (a
  // Linux guest also restarts capture on the modeset), so a mismatch only
  // counts as drift once it has outlived that. The key names one mismatch:
  // a new live mode, a new request, or a repeat of the same request starts
  // a new wait, and an expired key from an earlier mismatch never matches.
  const [resolutionRequests, setResolutionRequests] = useState(0);
  const driftKey = mismatched
    ? `${liveResolution}/${settings.resolution}/${resolutionRequests}`
    : null;
  const [expiredDriftKey] = useDebounceValue(driftKey, 5000);
  const currentResolution =
    driftKey !== null && expiredDriftKey === driftKey ? liveResolution : null;

  // Follow settings changed by another connected client.
  useEffect(() => {
    if (osServicesSettings) setSettings(osServicesSettings);
  }, [osServicesSettings]);

  const update = useCallback(
    (next: OsServicesSettings) => {
      if (!settings) return;
      const previous = settings;
      setSettings(next);
      setLoading(true);
      const sequenceAtSend = useVideoStore.getState().osServicesSettingsSequence;
      void send("setOsServicesSettings", { settings: next }, (response: JsonRpcResponse) => {
        setLoading(false);
        if ("error" in response) {
          if (!overtaken(sequenceAtSend)) setSettings(previous);
          notifications.error(
            m.os_services_save_failed({
              error: response.error.data || m.unknown_error(),
            }),
          );
          return;
        }
        if (!overtaken(sequenceAtSend)) setSettings(response.result as OsServicesSettings);
        notifications.success(m.os_services_saved());
      });
    },
    [send, settings, overtaken],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.os_services_title()} description={m.os_services_description()} />

      <Card>
        <div className="flex items-center gap-3 rounded p-4">
          <LuTriangleAlert className="h-5 w-5 flex-shrink-0 text-amber-600 dark:text-amber-400" />
          <p className="text-sm font-medium text-amber-800 dark:text-amber-200">
            {m.os_services_experimental()}
          </p>
        </div>
      </Card>

      {!settings ? (
        <SettingsItem
          title={m.os_services_title()}
          description={loadError ?? m.os_services_description()}
          loading={loading}
        >
          {!loading && (
            <Button
              size="SM"
              theme="light"
              text={m.retry()}
              onClick={load}
              data-testid="os-services-settings-retry"
            />
          )}
        </SettingsItem>
      ) : (
        <>
          {runningUpdate ? (
            <SettingsItem
              title={m.guest_update_settings_title()}
              description={guestUpdateDescription(runningUpdate)}
            >
              <Button
                size="SM"
                theme="primary"
                text={m.guest_update_install()}
                loading
                disabled
                data-testid="os-services-update-install"
              />
            </SettingsItem>
          ) : (
            osServices.sender && (
              <SettingsItem
                title={m.guest_update_settings_title()}
                description={
                  guestUpdate?.available && !guestUpdate.installable && installerProgress
                    ? installerProgress
                    : !guestUpdate?.available
                      ? m.guest_update_settings_current({ current: osServices.sender.version })
                      : guestUpdate.automatic && guestUpdate.installable
                        ? m.guest_update_settings_automatic({
                            current: osServices.sender.version,
                            version: guestUpdate.version,
                          })
                        : m.guest_update_settings_available({
                            current: osServices.sender.version,
                            version: guestUpdate.version,
                          })
                }
              >
                {guestUpdate?.available &&
                  (guestUpdate.installable ? (
                    <Button
                      size="SM"
                      theme="primary"
                      text={m.guest_update_install()}
                      onClick={startUpdate}
                      data-testid="os-services-update-install"
                    />
                  ) : (
                    <OsServicesInstallerButton
                      installer={installer}
                      version={guestUpdate.version}
                      theme="light"
                      testId="os-services-update-mount-installer"
                      menuTestId="os-services-update-installer-menu"
                    />
                  ))}
              </SettingsItem>
            )
          )}

          {!runningUpdate && !osServices.connected && !osServices.approvalPending && (
            <SettingsItem
              title={m.os_services_install_title()}
              description={installerProgress ?? m.os_services_install_description()}
            >
              <OsServicesInstallerButton
                installer={installer}
                theme="primary"
                testId="os-services-mount-installer"
                menuTestId="os-services-installer-menu"
              />
            </SettingsItem>
          )}

          {osServices.fingerprint && (
            <div className="flex items-center justify-between gap-x-8">
              <SettingsItem
                title={m.os_services_fingerprint_title()}
                description={
                  osServices.approvalPending ? (
                    <span className="text-amber-700 dark:text-amber-500">
                      {m.os_services_fingerprint_pending()}
                    </span>
                  ) : (
                    m.os_services_fingerprint_description()
                  )
                }
              />
              <div className="flex items-center">
                <GridCard cardClassName="rounded-r-none">
                  <div className="flex h-[34px] items-center px-3 font-mono text-xs whitespace-nowrap text-black select-all dark:text-white">
                    {osServices.fingerprint}
                  </div>
                </GridCard>
                <Button
                  className="rounded-l-none border-l-slate-800/30 dark:border-slate-300/20"
                  size="SM"
                  type="button"
                  theme="light"
                  LeadingIcon={LuCopy}
                  data-testid="os-services-fingerprint-copy"
                  onClick={async () => {
                    const id = osServices.fingerprint;
                    if (await copy(id)) {
                      notifications.success(m.os_services_fingerprint_copy_success({ id }));
                    } else {
                      notifications.error(m.os_services_fingerprint_copy_error());
                    }
                  }}
                />
              </div>
            </div>
          )}

          <div className="space-y-4">
            <SettingsSectionHeader title={m.os_services_video_section_title()} />

            <SettingsItem
              title={m.os_services_video_title()}
              description={
                videoUnavailable ? (
                  <>
                    {m.os_services_video_description()}
                    <span className="mt-0.5 block text-amber-700 dark:text-amber-500">
                      {osServices.videoError
                        ? m.os_services_video_faulted({
                            error: osServices.videoError,
                          })
                        : m.os_services_video_unregistered()}
                    </span>
                  </>
                ) : (
                  m.os_services_video_description()
                )
              }
              loading={loading}
            >
              <Checkbox
                checked={settings.video_enabled}
                disabled={loading}
                onChange={event => update({ ...settings, video_enabled: event.target.checked })}
              />
            </SettingsItem>

            <SettingsItem
              title={m.os_services_capture_title()}
              description={m.os_services_capture_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                fullWidth
                className="min-w-52"
                value={settings.capture_display}
                disabled={loading}
                options={[
                  { value: "", label: m.os_services_capture_auto() },
                  ...Array.from(
                    new Set([...(osServices.sender?.displays ?? []), settings.capture_display]),
                  )
                    .filter(Boolean)
                    .map(name => ({ value: name, label: name })),
                ]}
                onChange={event => update({ ...settings, capture_display: event.target.value })}
              />
            </SettingsItem>
            <SettingsItem
              title={m.os_services_resolution_title()}
              description={m.os_services_resolution_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                fullWidth
                className="min-w-52"
                value={currentResolution === null ? settings.resolution : currentResolutionValue}
                options={resolutionOptionsWith(currentResolution)}
                disabled={loading}
                onChange={event => {
                  if (event.target.value === currentResolutionValue) return;
                  setResolutionRequests(count => count + 1);
                  update({ ...settings, resolution: event.target.value });
                }}
              />
            </SettingsItem>

            <SettingsItem
              title={m.os_services_display_mode_title()}
              description={m.os_services_display_mode_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                fullWidth
                className="min-w-52"
                value={settings.display_mode}
                options={displayModeOptions}
                disabled={loading}
                onChange={event =>
                  update({
                    ...settings,
                    display_mode: event.target.value as OsServicesSettings["display_mode"],
                  })
                }
              />
            </SettingsItem>
          </div>

          <div className="space-y-4">
            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <SettingsSectionHeader title={m.os_services_access_title()} />
            {(
              [
                [
                  "terminal_enabled",
                  m.os_services_terminal_title(),
                  m.os_services_terminal_description(),
                ],
                ["files_enabled", m.os_services_files_title(), m.os_services_files_description()],
              ] as const
            ).map(([field, title, description]) => (
              <SettingsItem key={field} title={title} description={description} loading={loading}>
                <Checkbox
                  checked={settings[field]}
                  disabled={loading}
                  onChange={event => update({ ...settings, [field]: event.target.checked })}
                />
              </SettingsItem>
            ))}
          </div>

          <div className="space-y-4">
            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <SettingsSectionHeader
              title={m.os_services_clipboard_section_title()}
              description={m.os_services_clipboard_section_description({
                paste: clipboardShortcut("V"),
                copy: clipboardShortcut("C"),
              })}
            />

            <SettingsItem
              title={m.os_services_clipboard_title()}
              description={m.os_services_clipboard_description()}
              loading={loading}
            >
              <Checkbox
                checked={settings.clipboard_enabled}
                disabled={loading}
                onChange={event =>
                  update({
                    ...settings,
                    clipboard_enabled: event.target.checked,
                  })
                }
              />
            </SettingsItem>
          </div>
        </>
      )}
    </div>
  );
}
