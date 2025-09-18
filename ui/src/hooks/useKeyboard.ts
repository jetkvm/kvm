import { useCallback, useRef } from "react";

import {
  hidErrorRollOver,
  hidKeyBufferSize,
  KeysDownState,
  useHidStore,
  useRTCStore,
} from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidRpc } from "@/hooks/useHidRpc";
import {
  KeyboardLedStateMessage,
  KeyboardMacroStateMessage,
  KeyboardMacroStep,
  KeysDownStateMessage,
} from "@/hooks/hidRpc";
import { hidKeyToModifierMask, keys, modifiers } from "@/keyboardMappings";

const MACRO_RESET_KEYBOARD_STATE = {
  keys: new Array(hidKeyBufferSize).fill(0),
  modifier: 0,
  delay: 0,
};

export interface MacroStep {
  keys: string[] | null;
  modifiers: string[] | null;
  delay: number;
}

export type MacroSteps = MacroStep[];

const sleep = (ms: number): Promise<void> => new Promise(resolve => setTimeout(resolve, ms));

export default function useKeyboard() {
  const { send } = useJsonRpc();
  const { rpcDataChannel } = useRTCStore();
  const { keysDownState, setKeysDownState, setKeyboardLedState, setPasteModeEnabled } =
    useHidStore();

  const abortController = useRef<AbortController | null>(null);
  const setAbortController = useCallback((ac: AbortController | null) => {
    abortController.current = ac;
  }, []);

  // INTRODUCTION: The earlier version of the JetKVM device shipped with all keyboard state
  // being tracked on the browser/client-side. When adding the keyPressReport API to the
  // device-side code, we have to still support the situation where the browser/client-side code
  // is running on the cloud against a device that has not been updated yet and thus does not
  // support the keyPressReport API. In that case, we need to handle the key presses locally
  // and send the full state to the device, so it can behave like a real USB HID keyboard.
  // This flag indicates whether the keyPressReport API is available on the device which is
  // dynamically set when the device responds to the first key press event or reports its
  // keysDownState when queried since the keyPressReport was introduced together with the
  // getKeysDownState API.

  // HidRPC is a binary format for exchanging keyboard and mouse events
  const {
    reportKeyboardEvent: sendKeyboardEventHidRpc,
    reportKeypressEvent: sendKeypressEventHidRpc,
    reportKeyboardMacroEvent: sendKeyboardMacroEventHidRpc,
    cancelOngoingKeyboardMacro: cancelOngoingKeyboardMacroHidRpc,
    rpcHidReady,
  } = useHidRpc(message => {
    switch (message.constructor) {
      case KeysDownStateMessage:
        setKeysDownState((message as KeysDownStateMessage).keysDownState);
        break;
      case KeyboardLedStateMessage:
        setKeyboardLedState((message as KeyboardLedStateMessage).keyboardLedState);
        break;
      case KeyboardMacroStateMessage:
        if (!(message as KeyboardMacroStateMessage).isPaste) break;
        setPasteModeEnabled((message as KeyboardMacroStateMessage).state);
        break;
      default:
        break;
    }
  });

  const handleLegacyKeyboardReport = useCallback(
    async (keys: number[], modifier: number) => {
      send("keyboardReport", { keys, modifier }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.error(`Failed to send keyboard report ${keys} ${modifier}`, resp.error);
        }

        // On older backends, we need to set the keysDownState manually since without the hidRpc API, the state doesn't trickle down from the backend
        setKeysDownState({ modifier, keys });
      });
    },
    [send, setKeysDownState],
  );
  const sendKeystrokeLegacy = useCallback(async (keys: number[], modifier: number, ac?: AbortController) => {
    return await new Promise<void>((resolve, reject) => {
      const abortListener = () => {
        reject(new Error("Keyboard report aborted"));
      };

      ac?.signal?.addEventListener("abort", abortListener);

      send(
        "keyboardReport",
        { keys, modifier },
        params => {
          if ("error" in params) return reject(params.error);
          resolve();
        },
      );
    });
  }, [send]);

  // resetKeyboardState is used to reset the keyboard state to no keys pressed and no modifiers.
  // This is useful for macros and when the browser loses focus to ensure that the keyboard state
  // is clean.
  const resetKeyboardState = useCallback(async () => {
    // Reset the keys buffer to zeros and the modifier state to zero
    const { keys, modifier } = MACRO_RESET_KEYBOARD_STATE;
    if (rpcHidReady) {
      sendKeyboardEventHidRpc(keys, modifier);
    } else {
      // Older backends don't support the hidRpc API, so we send the full reset state
      handleLegacyKeyboardReport(keys, modifier);
    }
  }, [rpcHidReady, sendKeyboardEventHidRpc, handleLegacyKeyboardReport]);


  // executeMacro is used to execute a macro consisting of multiple steps.
  // Each step can have multiple keys, multiple modifiers and a delay.
  // The keys and modifiers are pressed together and held for the delay duration.
  // After the delay, the keys and modifiers are released and the next step is executed.
  // If a step has no keys or modifiers, it is treated as a delay-only step.
  // A small pause is added between steps to ensure that the device can process the events.
  const executeMacroRemote = useCallback(async (steps: MacroSteps) => {
    const macro: KeyboardMacroStep[] = [];

    for (const [_, step] of steps.entries()) {
      const keyValues = (step.keys || []).map(key => keys[key]).filter(Boolean);
      const modifierMask: number = (step.modifiers || [])
        .map(mod => modifiers[mod])
        .reduce((acc, val) => acc + val, 0);

      // If the step has keys and/or modifiers, press them and hold for the delay
      if (keyValues.length > 0 || modifierMask > 0) {
        macro.push({ keys: keyValues, modifier: modifierMask, delay: 20 });
        macro.push({ ...MACRO_RESET_KEYBOARD_STATE, delay: step.delay || 100 });
      }
    }

    sendKeyboardMacroEventHidRpc(macro);
  }, [sendKeyboardMacroEventHidRpc]);
  const executeMacroClientSide = useCallback(async (steps: MacroSteps) => {
    const promises: (() => Promise<void>)[] = [];

    const ac = new AbortController();
    setAbortController(ac);

    for (const [_, step] of steps.entries()) {
      const keyValues = (step.keys || []).map(key => keys[key]).filter(Boolean);
      const modifierMask: number = (step.modifiers || [])
        .map(mod => modifiers[mod])
        .reduce((acc, val) => acc + val, 0);

      // If the step has keys and/or modifiers, press them and hold for the delay
      if (keyValues.length > 0 || modifierMask > 0) {
        promises.push(() => sendKeystrokeLegacy(keyValues, modifierMask, ac));
        promises.push(() => resetKeyboardState());
        promises.push(() => sleep(step.delay || 100));
      }
    }

    const runAll = async () => {
      for (const promise of promises) {
        // Check if we've been aborted before executing each promise
        if (ac.signal.aborted) {
          throw new Error("Macro execution aborted");
        }
        await promise();
      }
    }

    return await new Promise<void>((resolve, reject) => {
      // Set up abort listener
      const abortListener = () => {
        reject(new Error("Macro execution aborted"));
      };

      ac.signal.addEventListener("abort", abortListener);

      runAll()
        .then(() => {
          ac.signal.removeEventListener("abort", abortListener);
          resolve();
        })
        .catch((error) => {
          ac.signal.removeEventListener("abort", abortListener);
          reject(error);
        });
    });
  }, [sendKeystrokeLegacy, resetKeyboardState, setAbortController]);
  const executeMacro = useCallback(async (steps: MacroSteps) => {
    if (rpcHidReady) {
      return executeMacroRemote(steps);
    }
    return executeMacroClientSide(steps);
  }, [rpcHidReady, executeMacroRemote, executeMacroClientSide]);

  const cancelExecuteMacro = useCallback(async () => {
    if (abortController.current) {
      abortController.current.abort();
    }
    if (!rpcHidReady) return;
    // older versions don't support this API,
    // and all paste actions are pure-frontend,
    // we don't need to cancel it actually
    cancelOngoingKeyboardMacroHidRpc();
  }, [rpcHidReady, cancelOngoingKeyboardMacroHidRpc, abortController]);

  // handleKeyPress is used to handle a key press or release event.
  // This function handle both key press and key release events.
  // It checks if the keyPressReport API is available and sends the key press event.
  // If the keyPressReport API is not available, it simulates the device-side key
  // handling for legacy devices and updates the keysDownState accordingly.
  // It then sends the full keyboard state to the device.
  const handleKeyPress = useCallback(
    async (key: number, press: boolean) => {
      if (rpcDataChannel?.readyState !== "open" && !rpcHidReady) return;
      if ((key || 0) === 0) return; // ignore zero key presses (they are bad mappings)

      if (rpcHidReady) {
        // if the keyPress api is available, we can just send the key press event
        // sendKeypressEvent is used to send a single key press/release event to the device.
        // It sends the key and whether it is pressed or released.
        // Older device version doesn't support this API, so we will switch to local key handling
        // In that case we will switch to local key handling and update the keysDownState
        // in client/browser-side code using simulateDeviceSideKeyHandlingForLegacyDevices.
        sendKeypressEventHidRpc(key, press);
      } else {
        // Older backends don't support the hidRpc API, so we need:
        // 1. Calculate the state
        // 2. Send the newly calculated state to the device
        const downState = simulateDeviceSideKeyHandlingForLegacyDevices(
          keysDownState,
          key,
          press,
        );

        handleLegacyKeyboardReport(downState.keys, downState.modifier);

        // if we just sent ErrorRollOver, reset to empty state
        if (downState.keys[0] === hidErrorRollOver) {
          resetKeyboardState();
        }
      }
    },
    [
      rpcDataChannel?.readyState,
      rpcHidReady,
      sendKeypressEventHidRpc,
      keysDownState,
      handleLegacyKeyboardReport,
      resetKeyboardState,
    ],
  );

  // IMPORTANT: See the keyPressReportApiAvailable comment above for the reason this exists
  function simulateDeviceSideKeyHandlingForLegacyDevices(
    state: KeysDownState,
    key: number,
    press: boolean,
  ): KeysDownState {
    // IMPORTANT: This code parallels the logic in the kernel's hid-gadget driver
    // for handling key presses and releases. It ensures that the USB gadget
    // behaves similarly to a real USB HID keyboard. This logic is paralleled
    // in the device-side code in hid_keyboard.go so make sure to keep them in sync.
    let modifiers = state.modifier;
    const keys = state.keys;
    const modifierMask = hidKeyToModifierMask[key] || 0;

    if (modifierMask !== 0) {
      // If the key is a modifier key, we update the keyboardModifier state
      // by setting or clearing the corresponding bit in the modifier byte.
      // This allows us to track the state of dynamic modifier keys like
      // Shift, Control, Alt, and Super.
      if (press) {
        modifiers |= modifierMask;
      } else {
        modifiers &= ~modifierMask;
      }
    } else {
      // handle other keys that are not modifier keys by placing or removing them
      // from the key buffer since the buffer tracks currently pressed keys
      let overrun = true;
      for (let i = 0; i < hidKeyBufferSize; i++) {
        // If we find the key in the buffer the buffer, we either remove it (if press is false)
        // or do nothing (if down is true) because the buffer tracks currently pressed keys
        // and if we find a zero byte, we can place the key there (if press is true)
        if (keys[i] === key || keys[i] === 0) {
          if (press) {
            keys[i] = key; // overwrites the zero byte or the same key if already pressed
          } else {
            // we are releasing the key, remove it from the buffer
            if (keys[i] !== 0) {
              keys.splice(i, 1);
              keys.push(0); // add a zero at the end
            }
          }
          overrun = false; // We found a slot for the key
          break;
        }
      }

      // If we reach here it means we didn't find an empty slot or the key in the buffer
      if (overrun) {
        if (press) {
          console.warn(
            `keyboard buffer overflow current keys ${keys}, key: ${key} not added`,
          );
          // Fill all key slots with ErrorRollOver (0x01) to indicate overflow
          keys.length = hidKeyBufferSize;
          keys.fill(hidErrorRollOver);
        } else {
          // If we are releasing a key, and we didn't find it in a slot, who cares?
          console.debug(`key ${key} not found in buffer, nothing to release`);
        }
      }
    }
    return { modifier: modifiers, keys };
  }

  return { handleKeyPress, resetKeyboardState, executeMacro, cancelExecuteMacro };
}
