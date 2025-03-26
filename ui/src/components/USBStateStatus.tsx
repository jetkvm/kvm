import React from "react";

import { cx } from "@/cva.config";
import KeyboardAndMouseConnectedIcon from "@/assets/keyboard-and-mouse-connected.png";
import LoadingSpinner from "@components/LoadingSpinner";
import StatusCard from "@components/StatusCards";
import { HidState } from "@/hooks/stores";

type USBStates = HidState["usbState"];

type StatusProps = Record<
  USBStates,
  {
    icon: React.FC<{ className: string | undefined }>;
    iconClassName: string;
    statusIndicatorClassName: string;
  }
>;

const USBStateMap: Record<USBStates, string> = {
  configured: "Connected",
  attached: "Connecting",
  addressed: "Connecting",
  "not attached": "Disconnected",
  suspended: "Low power mode",
};

export default function USBStateStatus({
  state,
  peerConnectionState,
}: {
  state: USBStates;
  peerConnectionState: RTCPeerConnectionState | null;
}) {
  const StatusCardProps: StatusProps = {
    configured: {
      icon: ({ className }) => (
        <img className={cx(className)} src={KeyboardAndMouseConnectedIcon} alt="" />
      ),
      iconClassName: "h-5 w-5 shrink-0",
      statusIndicatorClassName: "bg-green-500 border-green-600",
    },
    attached: {
      icon: ({ className }) => <LoadingSpinner className={cx(className)} />,
      iconClassName: "h-5 w-5 text-blue-500",
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    addressed: {
      icon: ({ className }) => <LoadingSpinner className={cx(className)} />,
      iconClassName: "h-5 w-5 text-blue-500",
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    "not attached": {
      icon: ({ className }) => (
        <img className={cx(className)} src={KeyboardAndMouseConnectedIcon} alt="" />
      ),
      iconClassName: "h-5 w-5 opacity-50 grayscale filter",
      statusIndicatorClassName: "bg-slate-300 border-slate-400",
    },
    suspended: {
      icon: ({ className }) => (
        <img className={cx(className)} src={KeyboardAndMouseConnectedIcon} alt="" />
      ),
      iconClassName: "h-5 w-5 opacity-50 grayscale filter",
      statusIndicatorClassName: "bg-green-500 border-green-600",
    },
  };
  const props = StatusCardProps[state];
  if (!props) {
    console.log("Unsupported USB state: ", state);
    return;
  }

  // If the peer connection is not connected, show the USB cable as disconnected
  if (peerConnectionState !== "connected") {
    const {
      icon: Icon,
      iconClassName,
      statusIndicatorClassName,
    } = StatusCardProps["not attached"];

    return (
      <StatusCard
        title="USB"
        status="Disconnected"
        icon={Icon}
        iconClassName={iconClassName}
        statusIndicatorClassName={statusIndicatorClassName}
      />
    );
  }

  return (
    <StatusCard title="USB" status={USBStateMap[state]} {...StatusCardProps[state]} />
  );
}
