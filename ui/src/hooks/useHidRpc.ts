import { useCallback, useEffect, useMemo } from "react";
import { Logger } from "tslog";

import { useRTCStore } from "@hooks/stores";

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

const HANDSHAKE_TIMEOUT = 30 * 1000; // 30 seconds
const HANDSHAKE_MAX_ATTEMPTS = 10;
const logger = new Logger({ name: "hidrpc" });

export function doRpcHidHandshake(
  rpcHidChannel: RTCDataChannel,
  setRpcHidProtocolVersion: (version: number | null) => void,
) {
  let attempts = 0;
  let lastConnectedTime: Date | undefined;
  let lastSendTime: Date | undefined;
  let handshakeCompleted = false;
  let handshakeInterval: ReturnType<typeof setInterval> | null = null;

  const shouldGiveUp = () => {
    if (attempts > HANDSHAKE_MAX_ATTEMPTS) {
      logger.error(`Failed to send handshake message after ${HANDSHAKE_MAX_ATTEMPTS} attempts`);
      return true;
    }

    const timeSinceConnected = lastConnectedTime ? Date.now() - lastConnectedTime.getTime() : 0;
    if (timeSinceConnected > HANDSHAKE_TIMEOUT) {
      logger.error(`Handshake timed out after ${timeSinceConnected}ms`);
      return true;
    }

    return false;
  };

  const sendHandshake = (initial: boolean) => {
    if (handshakeCompleted) return;

    attempts++;
    lastSendTime = new Date();

    if (!initial && shouldGiveUp()) {
      if (handshakeInterval) {
        clearInterval(handshakeInterval);
        handshakeInterval = null;
      }
      return;
    }

    let data: Uint8Array | undefined;
    try {
      const message = new HandshakeMessage(HID_RPC_VERSION);
      data = message.marshal();
    } catch (e) {
      logger.error("Failed to marshal message", e);
      return;
    }
    if (!data) return;
    rpcHidChannel.send(data as unknown as ArrayBuffer);

    if (initial) {
      handshakeInterval = setInterval(() => {
        sendHandshake(false);
      }, 1000);
    }
  };

  const onMessage = (ev: MessageEvent) => {
    const message = unmarshalHidRpcMessage(new Uint8Array(ev.data));
    if (!message || !(message instanceof HandshakeMessage)) return;

    if (!message.version) {
      logger.error("Received handshake message without version", message);
      return;
    }

    if (message.version > HID_RPC_VERSION) {
      // we assume that the UI is always using the latest version of the HID RPC protocol
      // so we can't support this
      // TODO: use capabilities to determine rather than version number
      logger.error("Server is using a newer version than the client", message);
      return;
    }

    setRpcHidProtocolVersion(message.version);

    const timeUsed = lastSendTime ? Date.now() - lastSendTime.getTime() : 0;
    logger.info(
      `Handshake completed in ${timeUsed}ms after ${attempts} attempts (Version: ${message.version} / ${HID_RPC_VERSION})`,
    );

    // clean up
    rpcHidChannel.removeEventListener("message", onMessage);
    resetHandshake({ completed: true });
  };

  const resetHandshake = ({
    lastConnectedTime: newLastConnectedTime,
    completed,
  }: {
    lastConnectedTime?: Date | undefined;
    completed?: boolean;
  }) => {
    if (newLastConnectedTime) lastConnectedTime = newLastConnectedTime;
    lastSendTime = undefined;
    attempts = 0;
    if (completed !== undefined) handshakeCompleted = completed;
    if (handshakeInterval) {
      clearInterval(handshakeInterval);
      handshakeInterval = null;
    }
  };

  const onConnected = () => {
    resetHandshake({ lastConnectedTime: new Date() });
    logger.info("Channel connected");

    sendHandshake(true);
    rpcHidChannel.addEventListener("message", onMessage);
  };

  const onClose = () => {
    resetHandshake({ lastConnectedTime: undefined, completed: false });

    logger.info("Channel closed");
    setRpcHidProtocolVersion(null);

    rpcHidChannel.removeEventListener("message", onMessage);
  };

  rpcHidChannel.addEventListener("open", onConnected);
  rpcHidChannel.addEventListener("close", onClose);

  // handle case where channel is already open when the hook is mounted
  if (rpcHidChannel.readyState === "open") {
    onConnected();
  }
}

export function useHidRpc(onHidRpcMessage?: (payload: RpcMessage) => void) {
  const {
    rpcHidChannel,
    rpcHidUnreliableChannel,
    rpcHidUnreliableNonOrderedChannel,
    setRpcHidProtocolVersion,
    rpcHidProtocolVersion,
    hidRpcDisabled,
  } = useRTCStore();

  const rpcHidReady = useMemo(() => {
    if (hidRpcDisabled) return false;
    return rpcHidChannel?.readyState === "open" && rpcHidProtocolVersion !== null;
  }, [rpcHidChannel, rpcHidProtocolVersion, hidRpcDisabled]);

  const rpcHidUnreliableReady = useMemo(() => {
    return rpcHidUnreliableChannel?.readyState === "open" && rpcHidProtocolVersion !== null;
  }, [rpcHidProtocolVersion, rpcHidUnreliableChannel?.readyState]);

  const rpcHidUnreliableNonOrderedReady = useMemo(() => {
    return (
      rpcHidUnreliableNonOrderedChannel?.readyState === "open" && rpcHidProtocolVersion !== null
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
      { ignoreHandshakeState, useUnreliableChannel, requireOrdered = true }: sendMessageParams = {},
    ) => {
      if (hidRpcDisabled) return;
      if (rpcHidChannel?.readyState !== "open") return;
      if (!rpcHidReady && !ignoreHandshakeState) return;

      let data: Uint8Array | undefined;
      try {
        data = message.marshal();
      } catch (e) {
        logger.error("Failed to marshal message", e);
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
      hidRpcDisabled,
      rpcHidUnreliableNonOrderedChannel,
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

  const cancelOngoingKeyboardMacro = useCallback(() => {
    sendMessage(new CancelKeyboardMacroReportMessage());
  }, [sendMessage]);

  const reportKeypressKeepAlive = useCallback(() => {
    sendMessage(KEEPALIVE_MESSAGE);
  }, [sendMessage]);

  useEffect(() => {
    if (!rpcHidChannel) return;
    if (hidRpcDisabled) return;

    const messageHandler = (e: MessageEvent) => {
      if (typeof e.data === "string") {
        logger.warn("Received string data in message handler", e.data);
        return;
      }

      const message = unmarshalHidRpcMessage(new Uint8Array(e.data));
      if (!message) {
        logger.warn("Received invalid message", e.data);
        return;
      }

      if (message instanceof HandshakeMessage) return; // handshake message is handled by the doRpcHidHandshake function

      // to remove it from the production build, we need to use the /* @__PURE__ */ comment here
      // setting `esbuild.pure` doesn't work
      /* @__PURE__ */ logger.debug("Received message", message);

      onHidRpcMessage?.(message);
    };

    const errorHandler = (e: Event) => {
      console.error(`Error on rpcHidChannel '${rpcHidChannel.label}': ${e}`);
    };

    rpcHidChannel.addEventListener("message", messageHandler);
    rpcHidChannel.addEventListener("error", errorHandler);

    return () => {
      rpcHidChannel.removeEventListener("message", messageHandler);
      rpcHidChannel.removeEventListener("error", errorHandler);
    };
  }, [rpcHidChannel, onHidRpcMessage, setRpcHidProtocolVersion, hidRpcDisabled]);

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
