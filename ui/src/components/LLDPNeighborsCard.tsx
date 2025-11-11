import { LLDPNeighbor } from "@hooks/stores";
import { GridCard } from "@components/Card";
import { cx } from "@/cva.config";
import { m } from "@localizations/messages";

interface LLDPDataLineProps {
  label: string;
  value: string;
  className?: string;
}
const LLDPDataLine = ({ label, value, className }: LLDPDataLineProps) => {
  return (
    <div className={cx("flex flex-col justify-between", className)}>
      <span className="text-sm text-slate-600 dark:text-slate-400">
        {label}
      </span>
      <span className="text-sm font-medium">{value}</span>
    </div>
  );
}

export default function LLDPNeighborsCard({
  neighbors,
}: {
  neighbors: LLDPNeighbor[];
}) {
  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-4">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">
            {m.network_ll_dp_neighbors()}
          </h3>

          <div className="space-y-3 pt-2">
            {neighbors.map(neighbor => {
              const displayName = neighbor.system_name || neighbor.port_description || neighbor.mac;
              const key = `${neighbor.mac}-${neighbor.source}`;
              return <div className="space-y-3" key={key}>
                <h4 className="text-sm font-semibold font-mono">{displayName}</h4>
                <div
                  className="rounded-md rounded-l-none border border-slate-500/10 border-l-blue-700/50 bg-white p-4 pl-4 backdrop-blur-sm dark:bg-transparent"
                >
                  <div className="grid grid-cols-2 gap-x-8 gap-y-4">

                    {neighbor.system_name && (
                      <LLDPDataLine label={m.network_ll_dp_system_name()} value={neighbor.system_name} />
                    )}

                    {neighbor.system_description && (
                      <LLDPDataLine label={m.network_ll_dp_system_description()} value={neighbor.system_description} className="col-span-2" />
                    )}

                    {neighbor.chassis_id && (
                      <LLDPDataLine label={m.network_ll_dp_chassis_id()} value={neighbor.chassis_id} />
                    )}

                    {neighbor.port_id && (
                      <LLDPDataLine label={m.network_ll_dp_port_id()} value={neighbor.port_id} />
                    )}

                    {neighbor.port_description && (
                      <LLDPDataLine label={m.network_ll_dp_port_description()} value={neighbor.port_description} />
                    )}

                    {neighbor.management_addresses && neighbor.management_addresses.length > 0 && (
                      neighbor.management_addresses.map((address, index) => (
                        <LLDPDataLine label={m.network_ll_dp_management_address()} value={address.address} key={index} />
                      ))
                    )}

                    {neighbor.mac && (
                      <LLDPDataLine label={m.network_ll_dp_mac_address()} value={neighbor.mac} className="font-mono" />
                    )}

                    {neighbor.source && (
                      <LLDPDataLine label={m.network_ll_dp_source()} value={neighbor.source} />
                    )}
                  </div>
                </div>
              </div>
            })}
          </div>
        </div>
      </div>
    </GridCard>
  );
}