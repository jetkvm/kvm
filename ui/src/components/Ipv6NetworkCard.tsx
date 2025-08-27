import { NetworkState } from "../hooks/stores";
import { LifeTimeLabel } from "../routes/devices.$id.settings.network";

import { GridCard } from "./Card";

export default function Ipv6NetworkCard({
  networkState,
}: {
  networkState: NetworkState;
}) {
  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            IPv6 Information
          </h3>

          <div className="grid grid-cols-2 gap-x-6 gap-y-2">
            {networkState?.ipv6_link_local && (
              <div className="flex flex-col justify-between">
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  Link-local
                </span>
                <span className="text-sm font-medium">
                  {networkState?.ipv6_link_local}
                </span>
              </div>
            )}
          </div>

          <div className="space-y-3 pt-2">
            {networkState?.ipv6_addresses && networkState?.ipv6_addresses.length > 0 && (
              <div className="space-y-3">
                <h4 className="text-sm font-semibold">IPv6 Addresses</h4>
                {networkState.ipv6_addresses.map(
                  addr => (
                    <div
                      key={addr.address}
                      className="rounded-md rounded-l-none border border-slate-500/10 border-l-blue-700/50 bg-white p-4 pl-4 backdrop-blur-sm dark:bg-transparent"
                    >
                      <div className="grid grid-cols-2 gap-x-8 gap-y-4">
                        <div className="col-span-2 flex flex-col justify-between">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Address
                          </span>
                          <span className="text-sm font-medium">{addr.address}</span>
                        </div>

                        {addr.valid_lifetime && (
                          <div className="flex flex-col justify-between">
                            <span className="text-sm text-slate-600 dark:text-slate-400">
                              Valid Lifetime
                            </span>
                            <span className="text-sm font-medium">
                              {addr.valid_lifetime === "" ? (
                                <span className="text-slate-400 dark:text-slate-600">
                                  N/A
                                </span>
                              ) : (
                                <LifeTimeLabel lifetime={`${addr.valid_lifetime}`} />
                              )}
                            </span>
                          </div>
                        )}
                        {addr.preferred_lifetime && (
                          <div className="flex flex-col justify-between">
                            <span className="text-sm text-slate-600 dark:text-slate-400">
                              Preferred Lifetime
                            </span>
                            <span className="text-sm font-medium">
                              {addr.preferred_lifetime === "" ? (
                                <span className="text-slate-400 dark:text-slate-600">
                                  N/A
                                </span>
                              ) : (
                                <LifeTimeLabel lifetime={`${addr.preferred_lifetime}`} />
                              )}
                            </span>
                          </div>
                        )}
                      </div>
                    </div>
                  ),
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
