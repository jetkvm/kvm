import { Fragment, useEffect } from "react";
import { LuCast, LuLoader, LuRefreshCw, LuStar } from "react-icons/lu";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";

import { cx } from "@/cva.config";
import { Button } from "@components/Button";
import { useCast, ChromecastDevice } from "@hooks/useCast";
import { useUiStore } from "@hooks/stores";

export default function CastButton() {
  const { setDisableVideoFocusTrap } = useUiStore();
  const {
    isCasting,
    activeDevice,
    discoveredDevices,
    isDiscovering,
    isStarting,
    error,
    preferredDevice,
    discoverDevices,
    startCasting,
    stopCasting,
    setPreferredDevice,
    refreshStatus,
  } = useCast();

  // Refresh status on mount
  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  return (
    <Popover>
      <PopoverButton as={Fragment}>
        <Button
          size="XS"
          theme="light"
          text={isCasting ? "Casting" : "Cast"}
          LeadingIcon={({ className }) => (
            <LuCast
              className={cx(className, {
                "text-blue-500": isCasting,
              })}
            />
          )}
          onClick={() => {
            setDisableVideoFocusTrap(true);
            if (!isCasting) {
              discoverDevices();
            }
          }}
        />
      </PopoverButton>
      <PopoverPanel
        anchor="bottom start"
        transition
        className={cx(
          "z-10 flex w-[300px] origin-top flex-col overflow-visible!",
          "flex origin-top flex-col transition duration-300 ease-out data-closed:translate-y-8 data-closed:opacity-0",
        )}
      >
        <div className="mx-auto w-full max-w-sm rounded-md border border-slate-200 bg-white p-3 shadow-lg dark:border-slate-700 dark:bg-slate-800">
          {isCasting ? (
            <CastingActive
              deviceName={activeDevice?.name || "Unknown"}
              onStop={stopCasting}
            />
          ) : (
            <DevicePicker
              devices={discoveredDevices}
              isDiscovering={isDiscovering}
              isStarting={isStarting}
              error={error}
              preferredDevice={preferredDevice}
              onSelect={startCasting}
              onSetPreferred={setPreferredDevice}
              onRefresh={discoverDevices}
            />
          )}
        </div>
      </PopoverPanel>
    </Popover>
  );
}

function CastingActive({
  deviceName,
  onStop,
}: {
  deviceName: string;
  onStop: () => void;
}) {
  return (
    <div className="space-y-2">
      <p className="text-sm font-medium text-slate-700 dark:text-slate-200">
        Casting to {deviceName}
      </p>
      <Button size="SM" theme="light" text="Stop Casting" onClick={onStop} fullWidth />
    </div>
  );
}

function DevicePicker({
  devices,
  isDiscovering,
  isStarting,
  error,
  preferredDevice,
  onSelect,
  onSetPreferred,
  onRefresh,
}: {
  devices: ChromecastDevice[];
  isDiscovering: boolean;
  isStarting: boolean;
  error: string | null;
  preferredDevice: { name: string; address: string; port: number } | null;
  onSelect: (device: ChromecastDevice) => void;
  onSetPreferred: (device: ChromecastDevice | null) => void;
  onRefresh: () => void;
}) {
  if (isStarting) {
    return (
      <div className="flex items-center gap-2 py-2">
        <LuLoader className="h-4 w-4 animate-spin text-blue-500" />
        <span className="text-sm text-slate-600 dark:text-slate-300">
          Starting stream...
        </span>
      </div>
    );
  }

  const isPreferred = (device: ChromecastDevice) =>
    preferredDevice?.address === device.address &&
    preferredDevice?.port === device.port;

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-slate-700 dark:text-slate-200">
          Chromecast Devices
        </span>
        <button
          onClick={onRefresh}
          disabled={isDiscovering}
          className="rounded p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
        >
          <LuRefreshCw
            className={cx("h-3.5 w-3.5", { "animate-spin": isDiscovering })}
          />
        </button>
      </div>

      {isDiscovering && devices.length === 0 && (
        <div className="flex items-center gap-2 py-2">
          <LuLoader className="h-4 w-4 animate-spin text-blue-500" />
          <span className="text-sm text-slate-500 dark:text-slate-400">
            Searching...
          </span>
        </div>
      )}

      {!isDiscovering && devices.length === 0 && !error && (
        <p className="py-2 text-sm text-slate-500 dark:text-slate-400">
          No devices found. Make sure your Chromecast is on the same network.
        </p>
      )}

      {error && (
        <p className="py-1 text-sm text-red-500">{error}</p>
      )}

      {devices.length > 0 && (
        <ul className="space-y-1">
          {devices.map(device => (
            <li
              key={device.uuid || device.address}
              className="group flex items-center rounded-md hover:bg-slate-100 dark:hover:bg-slate-700"
            >
              <button
                className="flex-1 px-2 py-1.5 text-left text-sm text-slate-700 dark:text-slate-200"
                onClick={() => onSelect(device)}
              >
                <span className="flex items-center gap-1.5">
                  {device.name || device.address}
                  {isPreferred(device) && (
                    <span className="text-xs text-amber-500">(preferred)</span>
                  )}
                </span>
              </button>
              <button
                className={cx(
                  "mr-1 rounded p-1 transition-colors",
                  isPreferred(device)
                    ? "text-amber-500 hover:text-amber-600"
                    : "text-slate-300 opacity-0 hover:text-amber-500 group-hover:opacity-100",
                )}
                onClick={e => {
                  e.stopPropagation();
                  onSetPreferred(isPreferred(device) ? null : device);
                }}
                title={
                  isPreferred(device)
                    ? "Remove as preferred device"
                    : "Set as preferred device"
                }
              >
                <LuStar
                  className={cx("h-3.5 w-3.5", {
                    "fill-current": isPreferred(device),
                  })}
                />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
