import { MdConnectWithoutContact } from "react-icons/md";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { Link } from "react-router-dom";
import { LuEllipsisVertical } from "react-icons/lu";

import Card from "@components/Card";
import { Button, LinkButton } from "@components/Button";

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
}: {
  title: string;
  id: string;
  online: boolean;
  lastSeen: Date | null;
}) {
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
                <div className="text-sm text-black dark:text-white">Online</div>
              </div>
            ) : (
              <div className="flex items-center gap-x-1.5">
                <div className="h-2.5 w-2.5 rounded-full border border-slate-400/60 dark:border-slate-500 bg-slate-200 dark:bg-slate-600" />
                <div className="text-sm text-black dark:text-white">
                  {lastSeen ? (
                    <>Last online {getRelativeTimeString(lastSeen)}</>
                  ) : (
                    <>Never seen online</>
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
                text="Connect to KVM"
                LeadingIcon={MdConnectWithoutContact}
                textAlign="center"
                to={`/devices/${id}`}
              />
            ) : (
              <Button
                size="MD"
                theme="light"
                text="Troubleshoot Connection"
                textAlign="center"
              />
            )}
          </div>
          <Menu as="div" className="relative inline-block text-left">
            <div>
              <MenuButton
                as={Button}
                theme="light"
                TrailingIcon={LuEllipsisVertical}
                size="MD"
              ></MenuButton>
            </div>

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
                            Rename
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
                            Deregister from cloud
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
