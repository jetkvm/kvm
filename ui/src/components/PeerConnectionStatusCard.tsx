import StatusCard from "@components/StatusCards";
import { m } from "@localizations/messages.js";

const PeerConnectionStatusMap = {
  connected: m.peer_connection_connected(),
  connecting: m.peer_connection_connecting(),
  disconnected: m.peer_connection_disconnected(),
  error: m.peer_connection_error(),
  closing: m.peer_connection_closing(),
  failed: m.peer_connection_failed(),
  closed: m.peer_connection_closed(),
  new: m.peer_connection_new(),
} as Record<RTCPeerConnectionState | "error" | "closing", string>;

export type PeerConnections = keyof typeof PeerConnectionStatusMap;

type StatusProps = Record<
  PeerConnections,
  {
    statusIndicatorClassName: string;
  }
>;

export default function PeerConnectionStatusCard({
  state,
  title,
}: {
  state?: RTCPeerConnectionState | null;
  title?: string;
}) {
  if (!state) return null;
  const StatusCardProps: StatusProps = {
    connected: {
      statusIndicatorClassName: "bg-green-500 border-green-600",
    },
    connecting: {
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    disconnected: {
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    error: {
      statusIndicatorClassName: "bg-red-500 border-red-600",
    },
    closing: {
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    failed: {
      statusIndicatorClassName: "bg-red-500 border-red-600",
    },
    closed: {
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    ["new"]: {
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
  };
  const props = StatusCardProps[state];
  if (!props) return;

  return (
    <StatusCard
      title={title || "JetKVM Device"}
      status={PeerConnectionStatusMap[state]}
      {...StatusCardProps[state]}
    />
  );
}
