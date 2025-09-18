import { useCallback, useEffect, useMemo } from "react";

import { useRTCStore } from "@/hooks/stores";

import {
  CancelKeyboardMacroReportMessage,
  HID_RPC_VERSION,
  HandshakeMessage,
  KeyboardMacroStep,
  KeyboardMacroReportMessage,
  KeyboardReportMessage,
  KeypressKeepAliveMessage,
  KeypressReportMessage,
  MouseReportMessage,
  PointerReportMessage,
  RpcMessage,
  unmarshalHidRpcMessage,
} from "./hidRpc";

const KEEPALIVE_MESSAGE = new KeypressKeepAliveMessage();

interface sendMessageParams {
  ignoreHandshakeState?: boolean;
  useUnreliableChannel?: boolean;
  requireOrdered?: boolean;
}

export function useHidRpc(onHidRpcMessage?: (payload: RpcMessage) => void) {
  const {
    rpcHidChannel,
    rpcHidUnreliableChannel,
    rpcHidUnreliableNonOrderedChannel,
    setRpcHidProtocolVersion,
    rpcHidProtocolVersion, hidRpcDisabled,
  } = useRTCStore();

  const rpcHidReady = useMemo(() => {
    if (hidRpcDisabled) return false;
    return rpcHidChannel?.readyState === "open" && rpcHidProtocolVersion !== null;
  }, [rpcHidChannel, rpcHidProtocolVersion, hidRpcDisabled]);

  const rpcHidUnreliableReady = useMemo(() => {
    return (
      rpcHidUnreliableChannel?.readyState === "open" && rpcHidProtocolVersion !== null
    );
  }, [rpcHidProtocolVersion, rpcHidUnreliableChannel?.readyState]);

  const rpcHidUnreliableNonOrderedReady = useMemo(() => {
    return (
      rpcHidUnreliableNonOrderedChannel?.readyState === "open" &&
      rpcHidProtocolVersion !== null
    );
  }, [rpcHidProtocolVersion, rpcHidUnreliableNonOrderedChannel?.readyState]);

  const rpcHidStatus = useMemo(() => {
    if (hidRpcDisabled) return "disabled";

    if (!rpcHidChannel) return "N/A";
    if (rpcHidChannel.readyState !== "open") return rpcHidChannel.readyState;
    if (!rpcHidProtocolVersion) return "handshaking";
    return `ready (v${rpcHidProtocolVersion}${rpcHidUnreliableReady ? "+u" : ""})`;
  }, [rpcHidChannel, rpcHidProtocolVersion, rpcHidUnreliableReady, hidRpcDisabled]);

  const sendMessage = useCallback(
    (
      message: RpcMessage,
      {
        ignoreHandshakeState,
        useUnreliableChannel,
        requireOrdered = true,
      }: sendMessageParams = {},
    ) => {
      if (hidRpcDisabled) return;
    if (rpcHidChannel?.readyState !== "open") return;
      if (!rpcHidReady && !ignoreHandshakeState) return;

      let data: Uint8Array | undefined;
      try {
        data = message.marshal();
      } catch (e) {
        console.error("Failed to send HID RPC message", e);
      }
      if (!data) return;

      if (useUnreliableChannel) {
        if (requireOrdered && rpcHidUnreliableReady) {
          rpcHidUnreliableChannel?.send(data as unknown as ArrayBuffer);
        } else if (!requireOrdered && rpcHidUnreliableNonOrderedReady) {
          rpcHidUnreliableNonOrderedChannel?.send(data as unknown as ArrayBuffer);
        }
        return;
      }

      rpcHidChannel?.send(data as unknown as ArrayBuffer);
    },
    [
      rpcHidChannel,
      rpcHidUnreliableChannel,
      hidRpcDisabled, rpcHidUnreliableNonOrderedChannel,
      rpcHidReady,
      rpcHidUnreliableReady,
      rpcHidUnreliableNonOrderedReady,
    ],
  );

  const reportKeyboardEvent = useCallback(
    (keys: number[], modifier: number) => {
      sendMessage(new KeyboardReportMessage(keys, modifier));
    },
    [sendMessage],
  );

  const reportKeypressEvent = useCallback(
    (key: number, press: boolean) => {
      sendMessage(new KeypressReportMessage(key, press));
    },
    [sendMessage],
  );

  const reportAbsMouseEvent = useCallback(
    (x: number, y: number, buttons: number) => {
      sendMessage(new PointerReportMessage(x, y, buttons), {
        useUnreliableChannel: true,
      });
    },
    [sendMessage],
  );

  const reportRelMouseEvent = useCallback(
    (dx: number, dy: number, buttons: number) => {
      sendMessage(new MouseReportMessage(dx, dy, buttons));
    },
    [sendMessage],
  );

  const reportKeyboardMacroEvent = useCallback(
    (steps: KeyboardMacroStep[]) => {
      sendMessage(new KeyboardMacroReportMessage(false, steps.length, steps));
    },
    [sendMessage],
  );

  const cancelOngoingKeyboardMacro = useCallback(
    () => {
      sendMessage(new CancelKeyboardMacroReportMessage());
    },
    [sendMessage],
  );

  const reportKeypressKeepAlive = useCallback(() => {
    sendMessage(KEEPALIVE_MESSAGE);
  }, [sendMessage]);

  const sendHandshake = useCallback(() => {
    if (hidRpcDisabled) return;
    if (rpcHidProtocolVersion) return;
    if (!rpcHidChannel) return;

    sendMessage(new HandshakeMessage(HID_RPC_VERSION), { ignoreHandshakeState: true });
  }, [rpcHidChannel, rpcHidProtocolVersion, sendMessage, hidRpcDisabled]);

  const handleHandshake = useCallback(
    (message: HandshakeMessage) => {
      if (hidRpcDisabled) return;

    if (!message.version) {
        console.error("Received handshake message without version", message);
        return;
      }

      if (message.version > HID_RPC_VERSION) {
        // we assume that the UI is always using the latest version of the HID RPC protocol
        // so we can't support this
        // TODO: use capabilities to determine rather than version number
        console.error("Server is using a newer HID RPC version than the client", message);
        return;
      }

      setRpcHidProtocolVersion(message.version);
    },
    [setRpcHidProtocolVersion, hidRpcDisabled],
  );

  useEffect(() => {
    if (!rpcHidChannel) return;
    if (hidRpcDisabled) return;

    // send handshake message
    sendHandshake();

    const messageHandler = (e: MessageEvent) => {
      if (typeof e.data === "string") {
        console.warn("Received string data in HID RPC message handler", e.data);
        return;
      }

      const message = unmarshalHidRpcMessage(new Uint8Array(e.data));
      if (!message) {
        console.warn("Received invalid HID RPC message", e.data);
        return;
      }

      console.debug("Received HID RPC message", message);
      switch (message.constructor) {
        case HandshakeMessage:
          handleHandshake(message as HandshakeMessage);
          break;
        default:
          // not all events are handled here, the rest are handled by the onHidRpcMessage callback
          break;
      }

      onHidRpcMessage?.(message);
    };

    const openHandler = () => {
      console.info("HID RPC channel opened");
      sendHandshake();
    };

    const closeHandler = () => {
      console.info("HID RPC channel closed");
      setRpcHidProtocolVersion(null);
    };

    rpcHidChannel.addEventListener("message", messageHandler);
    rpcHidChannel.addEventListener("close", closeHandler);
    rpcHidChannel.addEventListener("open", openHandler);

    return () => {
      rpcHidChannel.removeEventListener("message", messageHandler);
      rpcHidChannel.removeEventListener("close", closeHandler);
      rpcHidChannel.removeEventListener("open", openHandler);
    };
  }, [
    rpcHidChannel,
    onHidRpcMessage,
    setRpcHidProtocolVersion,
    sendHandshake,
    handleHandshake,
      hidRpcDisabled,
  ]);

  return {
    reportKeyboardEvent,
    reportKeypressEvent,
    reportAbsMouseEvent,
    reportRelMouseEvent,
    reportKeyboardMacroEvent,
    cancelOngoingKeyboardMacro,
    reportKeypressKeepAlive,
    rpcHidProtocolVersion,
    rpcHidReady,
    rpcHidStatus,
  };
}
