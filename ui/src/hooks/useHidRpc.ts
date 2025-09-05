import { useCallback, useEffect, useMemo } from "react";

import { useRTCStore } from "@/hooks/stores";

import {
  HID_RPC_VERSION,
  HandshakeMessage,
  KeyboardReportMessage,
  KeypressReportMessage,
  MouseReportMessage,
  PointerReportMessage,
  RpcMessage,
  unmarshalHidRpcMessage,
} from "./hidRpc";

export function useHidRpc(onHidRpcMessage?: (payload: RpcMessage) => void) {
  const { rpcHidChannel, setRpcHidProtocolVersion, rpcHidProtocolVersion } = useRTCStore();
  const rpcHidReady = useMemo(() => {
    return rpcHidChannel?.readyState === "open" && rpcHidProtocolVersion !== null;
  }, [rpcHidChannel, rpcHidProtocolVersion]);

  const rpcHidStatus = useMemo(() => {
    if (!rpcHidChannel) return "N/A";
    if (rpcHidChannel.readyState !== "open") return rpcHidChannel.readyState;
    if (!rpcHidProtocolVersion) return "handshaking";
    return `ready (v${rpcHidProtocolVersion})`;
  }, [rpcHidChannel, rpcHidProtocolVersion]);

  const sendMessage = useCallback((message: RpcMessage, ignoreHandshakeState = false) => {
    if (rpcHidChannel?.readyState !== "open") return;
    if (!rpcHidReady && !ignoreHandshakeState) return;

    let data: Uint8Array | undefined;
    try {
      data = message.marshal();
    } catch (e) {
      console.error("Failed to send HID RPC message", e);
    }
    if (!data) return;

    rpcHidChannel?.send(data as unknown as ArrayBuffer);
  }, [rpcHidChannel, rpcHidReady]);

  const reportKeyboardEvent = useCallback(
    (keys: number[], modifier: number) => {
      sendMessage(new KeyboardReportMessage(keys, modifier));
    }, [sendMessage],
  );

  const reportKeypressEvent = useCallback(
    (key: number, press: boolean) => {
      sendMessage(new KeypressReportMessage(key, press));
    },
    [sendMessage],
  );

  const reportAbsMouseEvent = useCallback(
    (x: number, y: number, buttons: number) => {
      sendMessage(new PointerReportMessage(x, y, buttons));
    },
    [sendMessage],
  );

  const reportRelMouseEvent = useCallback(
    (dx: number, dy: number, buttons: number) => {
      sendMessage(new MouseReportMessage(dx, dy, buttons));
    },
    [sendMessage],
  );

  const sendHandshake = useCallback(() => {
    if (rpcHidProtocolVersion) return;
    if (!rpcHidChannel) return;

    sendMessage(new HandshakeMessage(HID_RPC_VERSION), true);
  }, [rpcHidChannel, rpcHidProtocolVersion, sendMessage]);

  const handleHandshake = useCallback((message: HandshakeMessage) => {
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
  }, [setRpcHidProtocolVersion]);

  useEffect(() => {
    if (!rpcHidChannel) return;

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

    rpcHidChannel.addEventListener("message", messageHandler);

    return () => {
      rpcHidChannel.removeEventListener("message", messageHandler);
    };
  },
    [
      rpcHidChannel,
      onHidRpcMessage,
      setRpcHidProtocolVersion,
      sendHandshake,
      handleHandshake,
    ],
  );

  return {
    reportKeyboardEvent,
    reportKeypressEvent,
    reportAbsMouseEvent,
    reportRelMouseEvent,
    rpcHidProtocolVersion,
    rpcHidReady,
    rpcHidStatus,
  };
}
