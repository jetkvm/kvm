import { useEffect, useState } from "react";
import { LuPower, LuHardDrive } from "react-icons/lu";

import { useJsonRpc } from "@/hooks/useJsonRpc";

interface ATXState {
  power: boolean;
  hdd: boolean;
}

export default function ATXLeds() {
  const [state, setState] = useState<ATXState>({
    power: false,
    hdd: false,
  });

  const { send } = useJsonRpc(resp => {
    const params = resp?.params;

    if (params && typeof params.power === "boolean" && typeof params.hdd === "boolean") {
      setState(params as ATXState);
    }
  });

  useEffect(() => {
    send("getATXState", {}, resp => {
      if (
        !("error" in resp) &&
        resp.result &&
        typeof (resp.result as any).power === "boolean" &&
        typeof (resp.result as any).hdd === "boolean"
      ) {
        setState(resp.result as ATXState);
      }
    });
  }, [send]);

  const power = state?.power ?? false;
  const hdd = state?.hdd ?? false;

  return (
    <div className="flex items-center gap-3">
      <div
        className={`flex items-center gap-1 ${power ? "text-green-600" : "text-slate-300"}`}
        title={`Power: ${power ? "On" : "Off"}`}
      >
        <LuPower size={16} />
        <span className="text-xs font-medium">Power</span>
      </div>

      <div
        className={`flex items-center gap-1 ${hdd ? "text-blue-400" : "text-slate-300"}`}
        title={`HDD: ${hdd ? "Active" : "Idle"}`}
      >
        <LuHardDrive size={16} />
        <span className="text-xs font-medium">Disk</span>
      </div>
    </div>
  );
}
