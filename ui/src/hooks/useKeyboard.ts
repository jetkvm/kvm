import { useCallback } from "react";

import { KeysDownState, useHidStore, useRTCStore, hidKeyBufferSize, hidErrorRollOver } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { hidKeyToModifierMask, keys, modifiers } from "@/keyboardMappings";

export default function useKeyboard() {
  const { send } = useJsonRpc();
  const { rpcDataChannel } = useRTCStore();
  const { keysDownState, setKeysDownState } = useHidStore();

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
  const { keyPressReportApiAvailable, setkeyPressReportApiAvailable} = useHidStore();

  // sendKeyboardEvent is used to send the full keyboard state to the device for macro handling
  // and resetting keyboard state. It sends the keys currently pressed and the modifier state.
  // The device will respond with the keysDownState if it supports the keyPressReport API
  // or just accept the state if it does not support (returning no result)
  const sendKeyboardEvent = useCallback(
    async (state: KeysDownState) => {
      if (rpcDataChannel?.readyState !== "open") return;

      console.debug(`Send keyboardReport keys: ${state.keys}, modifier: ${state.modifier}`);
      send("keyboardReport", { keys: state.keys, modifier: state.modifier }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.error(`Failed to send keyboard report ${state}`, resp.error);
        } else {
          // If the device supports keyPressReport API, it will (also) return the keysDownState when we send
          // the keyboardReport
          const keysDownState = resp.result as KeysDownState;

          if (keysDownState) {
            setKeysDownState(keysDownState); // treat the response as the canonical state
            setkeyPressReportApiAvailable(true); // if they returned a keysDownState, we ALSO know they also support keyPressReport
          } else {
            // older devices versions do not return the keyDownState
            // so we just pretend they accepted what we sent
            setKeysDownState(state); 
            setkeyPressReportApiAvailable(false); // we ALSO know they do not support keyPressReport
          }
        }
      });
    },
    [rpcDataChannel?.readyState, send, setKeysDownState, setkeyPressReportApiAvailable],
  );

  // sendKeypressEvent is used to send a single key press/release event to the device.
  // It sends the key and whether it is pressed or released.
  // Older device version will not understand this request and will respond with
  // an error with code -32601, which means that the RPC method name was not recognized.
  // In that case we will switch to local key handling and update the keysDownState
  // in client/browser-side code using simulateDeviceSideKeyHandlingForLegacyDevices.
  const sendKeypressEvent = useCallback(
    async (key: number, press: boolean) => {
      if (rpcDataChannel?.readyState !== "open") return;

      console.debug(`Send keypressEvent key: ${key}, press: ${press}`);
      send("keypressReport", { key, press }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          // -32601 means the method is not supported because the device is running an older version
          if (resp.error.code === -32601) {
            console.error("Legacy device does not support keypressReport API, switching to local key down state handling", resp.error);
            setkeyPressReportApiAvailable(false);
          } else {
            console.error(`Failed to send key ${key} press: ${press}`, resp.error);
          }
        } else {
          const keysDownState = resp.result as KeysDownState;

          if (keysDownState) {
            setKeysDownState(keysDownState);
            // we don't need to set keyPressReportApiAvailable here, because it's already true or we never landed here
          }
        }
      });
    },
    [rpcDataChannel?.readyState, send, setkeyPressReportApiAvailable, setKeysDownState],
  );

  // resetKeyboardState is used to reset the keyboard state to no keys pressed and no modifiers.
  // This is useful for macros and when the browser loses focus to ensure that the keyboard state
  // is clean.
  const resetKeyboardState = useCallback(
    async () => {
      // Reset the keys buffer to zeros and the modifier state to zero
      keysDownState.keys.length = hidKeyBufferSize;
      keysDownState.keys.fill(0);
      keysDownState.modifier = 0;
      sendKeyboardEvent(keysDownState);
    }, [keysDownState, sendKeyboardEvent]);

  // executeMacro is used to execute a macro consisting of multiple steps.
  // Each step can have multiple keys, multiple modifiers and a delay.
  // The keys and modifiers are pressed together and held for the delay duration.
  // After the delay, the keys and modifiers are released and the next step is executed.
  // If a step has no keys or modifiers, it is treated as a delay-only step.
  // A small pause is added between steps to ensure that the device can process the events.
  const executeMacro = async (steps: { keys: string[] | null; modifiers: string[] | null; delay: number }[]) => {
    for (const [index, step] of steps.entries()) {
      const keyValues = (step.keys || []).map(key => keys[key]).filter(Boolean);
      const modifierMask: number = (step.modifiers || []).map(mod => modifiers[mod]).reduce((acc, val) => acc + val, 0);

      // If the step has keys and/or modifiers, press them and hold for the delay
      if (keyValues.length > 0 || modifierMask > 0) {
        sendKeyboardEvent({ keys: keyValues, modifier: modifierMask });
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

  // handleKeyPress is used to handle a key press or release event.
  // This function handle both key press and key release events.
  // It checks if the keyPressReport API is available and sends the key press event.
  // If the keyPressReport API is not available, it simulates the device-side key
  // handling for legacy devices and updates the keysDownState accordingly.
  // It then sends the full keyboard state to the device.
  const handleKeyPress = useCallback(
    async (key: number, press: boolean) => {
      if (rpcDataChannel?.readyState !== "open") return;
      if ((key || 0) === 0) return; // ignore zero key presses (they are bad mappings)

      if (keyPressReportApiAvailable) {
        // if the keyPress api is available, we can just send the key press event
        sendKeypressEvent(key, press);
      } else {
        // if the keyPress api is not available, we need to handle the key locally
        const downState = simulateDeviceSideKeyHandlingForLegacyDevices(keysDownState, key, press);
        sendKeyboardEvent(downState); // then we send the full state

        // if we just sent ErrorRollOver, reset to empty state
        if (downState.keys[0] === hidErrorRollOver) {
          resetKeyboardState();
        }
      }
    },
    [keyPressReportApiAvailable, keysDownState, resetKeyboardState, rpcDataChannel?.readyState, sendKeyboardEvent, sendKeypressEvent],
  );

  // IMPORTANT: See the keyPressReportApiAvailable comment above for the reason this exists
  function simulateDeviceSideKeyHandlingForLegacyDevices(state: KeysDownState, key: number, press: boolean): KeysDownState {
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
            keys[i] = key // overwrites the zero byte or the same key if already pressed
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
          console.warn(`keyboard buffer overflow current keys ${keys}, key: ${key} not added`);
          // Fill all key slots with ErrorRollOver (0x01) to indicate overflow
          keys.length = hidKeyBufferSize;
          keys.fill(hidErrorRollOver);
        } else {
          // If we are releasing a key, and we didn't find it in a slot, who cares?
          console.debug(`key ${key} not found in buffer, nothing to release`)
        }
      }
    }
    return { modifier: modifiers, keys };
  }

  return { handleKeyPress, resetKeyboardState, executeMacro };
}
