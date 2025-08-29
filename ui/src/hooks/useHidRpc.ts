import { useCallback, useEffect, useState } from "react";

import { KeysDownState, useRTCStore } from "@/hooks/stores";

export const HID_RPC_MESSAGE_TYPES = {
  Handshake: 0x01,
  KeyboardReport: 0x02,
  PointerReport: 0x03,
  WheelReport: 0x04,
  KeypressReport: 0x05,
  MouseReport: 0x06,
  KeyboardLedState: 0x32,
  KeysDownState: 0x33,
}

export type HidRpcMessageType = typeof HID_RPC_MESSAGE_TYPES[keyof typeof HID_RPC_MESSAGE_TYPES];

const withinUint8Range = (value: number) => {
  return value >= 0 && value <= 255;
};

const fromInt32toUint8 = (n: number) => {
  if (n !== n >> 0) {
    throw new Error(`Number ${n} is not within the int32 range`);
  }

  return new Uint8Array([
    (n >> 24) & 0xFF,
    (n >> 16) & 0xFF,
    (n >> 8) & 0xFF,
    (n >> 0) & 0xFF,
  ]);
};

const fromInt8ToUint8 = (n: number) => {
  if (n < -128 || n > 127) {
    throw new Error(`Number ${n} is not within the int8 range`);
  }

  return (n >> 0) & 0xFF;
};

const toPointerReportEvent = (x: number, y: number, buttons: number) => {
  if (!withinUint8Range(buttons)) {
    throw new Error(`Buttons ${buttons} is not within the uint8 range`);
  }

  return new Uint8Array([
    HID_RPC_MESSAGE_TYPES.PointerReport,
    ...fromInt32toUint8(x),
    ...fromInt32toUint8(y),
    buttons,
  ]);
};

const toMouseReportEvent = (dx: number, dy: number, buttons: number) => {
  if (!withinUint8Range(buttons)) {
    throw new Error(`Buttons ${buttons} is not within the uint8 range`);
  }
  return new Uint8Array([
    HID_RPC_MESSAGE_TYPES.MouseReport,
    fromInt8ToUint8(dx),
    fromInt8ToUint8(dy),
    buttons,
  ]);
};

const toKeyboardReportEvent = (keys: number[], modifier: number) => {
  if (!withinUint8Range(modifier)) {
    throw new Error(`Modifier ${modifier} is not within the uint8 range`);
  }

  keys.forEach((k) => {
    if (!withinUint8Range(k)) {
      throw new Error(`Key ${k} is not within the uint8 range`);
    }
  });

  return new Uint8Array([
    HID_RPC_MESSAGE_TYPES.KeyboardReport,
    modifier,
    ...keys,
  ]);
};

const toKeypressReportEvent = (key: number, press: boolean) => {
  if (!withinUint8Range(key)) {
    throw new Error(`Key ${key} is not within the uint8 range`);
  }

  return new Uint8Array([
    HID_RPC_MESSAGE_TYPES.KeypressReport,
    key,
    press ? 1 : 0,
  ]);
};

const toHandshakeMessage = () => {
  return new Uint8Array([HID_RPC_MESSAGE_TYPES.Handshake]);
};

export interface HidRpcMessage {
  type: HidRpcMessageType;
  keysDownState?: KeysDownState;
}

const unmarshalHidRpcMessage = (data: Uint8Array): HidRpcMessage | undefined => {
  if (data.length < 1) {
    throw new Error(`Invalid HID RPC message length: ${data.length}`);
  }

  const payload = data.slice(1);

  switch (data[0]) {
    case HID_RPC_MESSAGE_TYPES.Handshake:
      return {
        type: HID_RPC_MESSAGE_TYPES.Handshake,
      };
    case HID_RPC_MESSAGE_TYPES.KeysDownState:
      return {
        type: HID_RPC_MESSAGE_TYPES.KeysDownState,
        keysDownState: {
          modifier: payload[0],
          keys: Array.from(payload.slice(1))
        },
      };
    default:
      throw new Error(`Unknown HID RPC message type: ${data[0]}`);
  }
};

export function useHidRpc(onHidRpcMessage?: (payload: HidRpcMessage) => void) {
  const { rpcHidChannel } = useRTCStore();
  const [handshakeCompleted, setHandshakeCompleted] = useState(false);

  const reportKeyboardEvent = useCallback(
    (keys: number[], modifier: number) => {
      if (rpcHidChannel?.readyState !== "open") return;
      rpcHidChannel.send(toKeyboardReportEvent(keys, modifier));
    },
    [rpcHidChannel],
  );

  const reportKeypressEvent = useCallback(
    (key: number, press: boolean) => {
      if (rpcHidChannel?.readyState !== "open") return;
      rpcHidChannel.send(toKeypressReportEvent(key, press));
    },
    [rpcHidChannel],
  );

  const reportAbsMouseEvent = useCallback(
    (x: number, y: number, buttons: number) => {
      if (rpcHidChannel?.readyState !== "open") return;
      rpcHidChannel.send(toPointerReportEvent(x, y, buttons));
    },
    [rpcHidChannel],
  );

  const reportRelMouseEvent = useCallback(
    (dx: number, dy: number, buttons: number) => {
      if (rpcHidChannel?.readyState !== "open") return;
      rpcHidChannel.send(toMouseReportEvent(dx, dy, buttons));
    },
    [rpcHidChannel],
  );

  const doHandshake = useCallback(() => {
    if (handshakeCompleted) return;
    if (!rpcHidChannel) return;

    rpcHidChannel.send(toHandshakeMessage());
  }, [rpcHidChannel, handshakeCompleted]);

  useEffect(() => {
    if (!rpcHidChannel) return;

    // send handshake message
    doHandshake();

    const messageHandler = (e: MessageEvent) => {
      if (typeof e.data === "string") {
        console.warn("Received string data in HID RPC message handler", e.data);
        return;
      }

      console.log("Received HID RPC message", e.data);

      const message = unmarshalHidRpcMessage(new Uint8Array(e.data));
      if (!message) {
        console.warn("Received invalid HID RPC message", e.data);
        return;
      }

      if (message.type === HID_RPC_MESSAGE_TYPES.Handshake) {
        setHandshakeCompleted(true);
      }

      onHidRpcMessage?.(message);
    };

    rpcHidChannel.addEventListener("message", messageHandler);

    return () => {
      rpcHidChannel.removeEventListener("message", messageHandler);
    };
  },
    [rpcHidChannel, onHidRpcMessage, setHandshakeCompleted, doHandshake]);

  return {
    reportKeyboardEvent,
    reportKeypressEvent,
    reportAbsMouseEvent,
    reportRelMouseEvent,
    handshakeCompleted,
  };
}
