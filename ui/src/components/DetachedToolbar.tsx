import { LuX } from "react-icons/lu";
import { cx } from "@/cva.config";
import { m } from "@localizations/messages.js";

interface DetachedToolbarProps {
  deviceName: string;
  connectionState: RTCPeerConnectionState | null;
  onClose: () => void;
}

function ConnectionStatusDot({ state }: { state: RTCPeerConnectionState | null }) {
  const getColor = () => {
    switch (state) {
      case "connected":
        return "bg-green-500";
      case "connecting":
      case "new":
        return "bg-yellow-500";
      case "disconnected":
      case "failed":
      case "closed":
        return "bg-red-500";
      default:
        return "bg-gray-500";
    }
  };

  const getLabel = () => {
    switch (state) {
      case "connected":
        return m.peer_connection_connected();
      case "connecting":
        return m.peer_connection_connecting();
      case "new":
        return m.peer_connection_new();
      case "disconnected":
        return m.peer_connection_disconnected();
      case "failed":
        return m.peer_connection_failed();
      case "closed":
        return m.peer_connection_closed();
      default:
        return m.peer_connection_connecting();
    }
  };

  return (
    <div className="flex items-center gap-2">
      <div className={cx("h-2 w-2 rounded-full", getColor())} />
      <span className="text-xs text-slate-400">{getLabel()}</span>
    </div>
  );
}

export default function DetachedToolbar({
  deviceName,
  connectionState,
  onClose,
}: DetachedToolbarProps) {
  return (
    <div className="flex h-8 shrink-0 items-center justify-between bg-slate-900 px-3 text-white">
      <span className="max-w-[200px] truncate text-sm font-medium">{deviceName}</span>
      <ConnectionStatusDot state={connectionState} />
      <button
        onClick={onClose}
        className="flex items-center justify-center rounded p-1 transition-colors hover:bg-slate-700"
        title={m.close()}
      >
        <LuX className="h-4 w-4" />
      </button>
    </div>
  );
}
