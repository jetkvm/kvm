import { Link } from "react-router";
import { MdConnectWithoutContact } from "react-icons/md";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { LuEllipsisVertical } from "react-icons/lu";
import semver from "semver";
import { useMemo } from "react";

import Card from "@components/Card";
import { Button, LinkButton } from "@components/Button";
import { m } from "@localizations/messages.js";
import { CLOUD_BACKWARDS_COMPATIBLE_VERSION, CLOUD_ENABLE_VERSIONED_UI } from "@/ui.config";

function getRelativeTimeString(date: Date | number, lang = navigator.language): string {
  // Allow dates or times to be passed
  const timeMs = typeof date === "number" ? date : date.getTime();

  // Get the amount of seconds between the given date and now
  const deltaSeconds = Math.round((timeMs - Date.now()) / 1000);

  // Array representing one minute, hour, day, week, month, etc in seconds
  const cutoffs = [60, 3600, 86400, 86400 * 7, 86400 * 30, 86400 * 365, Infinity];

  // Array equivalent to the above but in the string representation of the units
  const units: Intl.RelativeTimeFormatUnit[] = [
    "second",
    "minute",
    "hour",
    "day",
    "week",
    "month",
    "year",
  ];

  // Grab the ideal cutoff unit
  const unitIndex = cutoffs.findIndex(cutoff => cutoff > Math.abs(deltaSeconds));

  // Get the divisor to divide from the seconds. E.g. if our unit is "day" our divisor
  // is one day in seconds, so we can divide our seconds by this to get the # of days
  const divisor = unitIndex ? cutoffs[unitIndex - 1] : 1;

  // Intl.RelativeTimeFormat do its magic
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });
  return rtf.format(Math.floor(deltaSeconds / divisor), units[unitIndex]);
}

export default function KvmCard({
  title,
  id,
  online,
  lastSeen,
  appVersion,
}: {
  title: string;
  id: string;
  online: boolean;
  lastSeen: Date | null;
  appVersion?: string;
}) {
  /**
   * Constructs the URL for connecting to this KVM device's interface.
   *
   * CLOUD_BACKWARDS_COMPATIBLE_VERSION is the last backwards-compatible UI that works with older devices.
   * Devices on CLOUD_BACKWARDS_COMPATIBLE_VERSION or below are served that version, while newer devices get
   * their actual version. Unparseable versions fall back to CLOUD_BACKWARDS_COMPATIBLE_VERSION for safety.
   */
  const kvmUrl = useMemo(() => {
    let uri = `/devices/${id}`;

    // Only use versioned path if versioned UI is enabled
    if (CLOUD_ENABLE_VERSIONED_UI) {
      // Use device version if valid and >= 0.5.0, otherwise fall back to backwards-compatible version
      let version = CLOUD_BACKWARDS_COMPATIBLE_VERSION;
      if (appVersion && semver.valid(appVersion) && semver.gte(appVersion, CLOUD_BACKWARDS_COMPATIBLE_VERSION)) {
        version = appVersion;
      }
      uri = `/v/${version}${uri}`;
    }

    return new URL(uri, window.location.origin).toString();
  }, [appVersion, id]);


  return (
    <Card>
      <div className="px-5 py-5 space-y-3">
        <div className="flex justify-between items-center">
          <div className="space-y-1.5">
            <div className="text-lg font-bold leading-none text-black dark:text-white">
              {title}
            </div>

            {online ? (
              <div className="flex items-center gap-x-1.5">
                <div className="h-2.5 w-2.5 rounded-full border border-green-600 bg-green-500" />
                <div className="text-sm text-black dark:text-white">{m.online()}</div>
              </div>
            ) : (
              <div className="flex items-center gap-x-1.5">
                <div className="h-2.5 w-2.5 rounded-full border border-slate-400/60 dark:border-slate-500 bg-slate-200 dark:bg-slate-600" />
                <div className="text-sm text-black dark:text-white">
                  {lastSeen ? (
                    <>{m.last_online({ time: getRelativeTimeString(lastSeen) })}</>
                  ) : (
                    <>{m.never_seen_online()}</>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
        <div className="h-px bg-slate-800/20 dark:bg-slate-300/20" />
        <div className="flex justify-between">
          <div>
            {online ? (
              <LinkButton
                size="MD"
                theme="light"
                text={m.connect_to_kvm()}
                LeadingIcon={MdConnectWithoutContact}
                textAlign="center"
                reloadDocument
                target="_self"
                to={kvmUrl}
              />
            ) : (
              <Button
                size="MD"
                theme="light"
                text={m.troubleshoot_connection()}
                textAlign="center"
              />
            )}
          </div>
          <Menu as="div" className="relative inline-block text-left">
            <MenuButton
              as={Button}
              theme="light"
              TrailingIcon={LuEllipsisVertical}
              size="MD"
            ></MenuButton>
            <MenuItems
              transition
              className="data-closed:scale-95 data-closed:transform data-closed:opacity-0 data-enter:duration-100 data-leave:duration-75 data-enter:ease-out data-leave:ease-in"
            >
              <Card className="absolute right-0 z-10 w-56 px-1 mt-2 transition origin-top-right ring-1 ring-black/50 focus:outline-hidden">
                <div className="divide-y divide-slate-800/20 dark:divide-slate-300/20">
                  <MenuItem>
                    <div>
                      <div className="block w-full">
                        <div className="flex items-center px-2 my-1 text-sm transition-colors rounded-md gap-x-2 hover:bg-slate-100 dark:hover:bg-slate-700">
                          <Link
                            className="block w-full py-1.5 text-black dark:text-white"
                            to={`./${id}/rename`}
                          >
                            {m.rename_device()}
                          </Link>
                        </div>
                      </div>
                    </div>
                  </MenuItem>
                  <MenuItem>
                    <div>
                      <div className="block w-full">
                        <div className="flex items-center px-2 my-1 text-sm transition-colors rounded-md gap-x-2 hover:bg-slate-100 dark:hover:bg-slate-700">
                          <Link
                            className="block w-full py-1.5 text-black dark:text-white"
                            to={`./${id}/deregister`}
                          >
                            {m.deregister_from_cloud()}
                          </Link>
                        </div>
                      </div>
                    </div>
                  </MenuItem>
                </div>
              </Card>
            </MenuItems>
          </Menu>
        </div>
      </div>
    </Card>
  );
}
