import {useTranslation} from "react-i18next";

import StatusCard from "@components/StatusCards";


const PeerConnectionStatusMap = {
  connected: "Connected",
  connecting: "Connecting",
  disconnected: "Disconnected",
  error: "Connection_error",
  closing: "Closing",
  failed: "Connection_failed",
  closed: "Closed",
  new: "Connecting",
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
  if (!state) return;
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const { t } = useTranslation();
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
      status={t(PeerConnectionStatusMap[state])}
      {...StatusCardProps[state]}
    />
  );
}