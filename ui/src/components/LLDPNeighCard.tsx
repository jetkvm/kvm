import { LLDPNeighbor } from "../hooks/stores";
import { LifeTimeLabel } from "../routes/devices.$id.settings.network";

import { GridCard } from "./Card";

export default function LLDPNeighCard({
  neighbors,
}: {
  neighbors: LLDPNeighbor[];
}) {
  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            LLDP Neighbors
          </h3>

          <div className="space-y-3 pt-2">
            {neighbors.map(neighbor => (
              <div className="space-y-3" key={neighbor.mac}>
                <h4 className="text-sm font-semibold font-mono">{neighbor.mac}</h4>
                <div
                  className="rounded-md rounded-l-none border border-slate-500/10 border-l-blue-700/50 bg-white p-4 pl-4 backdrop-blur-sm dark:bg-transparent"
                >
                  <div className="grid grid-cols-2 gap-x-8 gap-y-4">
                    <div className="col-span-2 flex flex-col justify-between">
                      <span className="text-sm text-slate-600 dark:text-slate-400">
                        Interface
                      </span>
                      <span className="text-sm font-medium">{neighbor.port_description}</span>
                    </div>

                    {neighbor.system_name && (
                      <div className="flex flex-col justify-between">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          System Name
                        </span>
                        <span className="text-sm font-medium">{neighbor.system_name}</span>
                      </div>
                    )}

                    {neighbor.system_description && (
                      <div className="col-span-2 flex flex-col justify-between">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          System Description
                        </span>
                        <span className="text-sm font-medium">{neighbor.system_description}</span>
                      </div>
                    )}


                    {neighbor.port_id && (
                      <div className="flex flex-col justify-between">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          Port ID
                        </span>
                        <span className="text-sm font-medium">
                          {neighbor.port_id}
                        </span>
                      </div>
                    )}


                    {neighbor.port_description && (
                      <div className="flex flex-col justify-between">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          Port Description
                        </span>
                        <span className="text-sm font-medium">
                          {neighbor.port_description}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
