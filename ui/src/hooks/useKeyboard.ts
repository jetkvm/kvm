import { useCallback } from "react";

import { useHidStore, useRTCStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";

export default function useKeyboard() {
  const [send] = useJsonRpc();

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const updateActiveKeysAndModifiers = useHidStore(
    state => state.updateActiveKeysAndModifiers,
  );

  const sendKeyboardEvent = useCallback(
    (keys: number[], modifiers: number[]) => {
      if (rpcDataChannel?.readyState !== "open") return;
      const accModifier = modifiers.reduce((acc, val) => acc + val, 0);

      send("keyboardReport", { keys, modifier: accModifier });

      // We do this for the info bar to display the currently pressed keys for the user
      updateActiveKeysAndModifiers({ keys: keys, modifiers: modifiers });
    },
    [rpcDataChannel?.readyState, send, updateActiveKeysAndModifiers],
  );

  const resetKeyboardState = useCallback(() => {
    sendKeyboardEvent([], []);
  }, [sendKeyboardEvent]);

  return { sendKeyboardEvent, resetKeyboardState };
}
