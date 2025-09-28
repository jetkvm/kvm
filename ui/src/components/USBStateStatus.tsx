import React from "react";
import { useTranslation } from "react-i18next";

import { cx } from "@/cva.config";
import KeyboardAndMouseConnectedIcon from "@/assets/keyboard-and-mouse-connected.png";
import LoadingSpinner from "@components/LoadingSpinner";
import StatusCard from "@components/StatusCards";
import { USBStates } from "@/hooks/stores";

type StatusProps = Record<
  USBStates,
  {
    icon: React.FC<{ className: string | undefined }>;
    iconClassName: string;
    statusIndicatorClassName: string;
  }
>;
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

export default function USBStateStatus({
  state,
  peerConnectionState,
}: {
  state: USBStates;
  peerConnectionState?: RTCPeerConnectionState | null;
}) {
  const { t } = useTranslation();
  const USBStateMap: Record<USBStates, string> = {
        configured: t('Connected'),
        attached: t('Connecting'),
        addressed: t('Connecting'),
        "not attached": t('Disconnected'),
        suspended: t('Low_power_mode'),
  };
  const props = StatusCardProps[state];
  if (!props) {
    console.warn("Unsupported USB state: ", state);
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
        status={t('Disconnected')}
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
