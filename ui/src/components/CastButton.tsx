import { Fragment, useEffect } from "react";
import { LuCast, LuLoader, LuRefreshCw } from "react-icons/lu";
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
    discoverDevices,
    startCasting,
    stopCasting,
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
              onSelect={startCasting}
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
  onSelect,
  onRefresh,
}: {
  devices: ChromecastDevice[];
  isDiscovering: boolean;
  isStarting: boolean;
  error: string | null;
  onSelect: (device: ChromecastDevice) => void;
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
            <li key={device.uuid || device.address}>
              <button
                className="w-full rounded-md px-2 py-1.5 text-left text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-700"
                onClick={() => onSelect(device)}
              >
                {device.name || device.address}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
