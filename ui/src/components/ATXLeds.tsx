import { useEffect, useState } from "react";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { LuPower, LuHardDrive } from "react-icons/lu";

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
    setState(resp.params as ATXState);
  });

  useEffect(() => {
    send("getATXState", {}, resp => {
      if (!("error" in resp)) {
        setState(resp.result as ATXState);
      }
    });
  }, [send]);

  return (
    <div className="flex items-center gap-3">
      <div
        className={`flex items-center gap-1 ${state.power ? "text-green-600" : "text-slate-300"}`}
        title={`Power: ${state.power ? "On" : "Off"}`}
      >
        <LuPower size={16} />
        <span className="text-xs font-medium">Power</span>
      </div>

      <div
        className={`flex items-center gap-1 ${state.hdd ? "text-blue-400" : "text-slate-300"}`}
        title={`HDD: ${state.hdd ? "Active" : "Idle"}`}
      >
        <LuHardDrive size={16} />
        <span className="text-xs font-medium">Disk </span>
      </div>
    </div>
  );
}
