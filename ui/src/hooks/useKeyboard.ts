import { useCallback } from "react";

import { useHidStore, useRTCStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { keys, modifiers } from "@/keyboardMappings";

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

  const executeMacro = async (steps: { keys: string[] | null; modifiers: string[] | null; delay: number }[]) => {
    for (const [index, step] of steps.entries()) {
      const keyValues = step.keys?.map(key => keys[key]).filter(Boolean) || [];
      const modifierValues = step.modifiers?.map(mod => modifiers[mod]).filter(Boolean) || [];

      // If the step has keys and/or modifiers, press them and hold for the delay
      if (keyValues.length > 0 || modifierValues.length > 0) {
        sendKeyboardEvent(keyValues, modifierValues);
        await new Promise(resolve => setTimeout(resolve, step.delay || 50));

        resetKeyboardState();
      } else {
        // This is a delay-only step, just wait for the delay amount
        await new Promise(resolve => setTimeout(resolve, step.delay || 50));
      }

      // Add a small pause between steps if not the last step
      if (index < steps.length - 1) {
        await new Promise(resolve => setTimeout(resolve, 10));
      }
    }
  };

  return { sendKeyboardEvent, resetKeyboardState, executeMacro };
}
