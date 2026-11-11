import { useMemo } from "react";

import { useSettingsStore } from "@/hooks/stores";
import { keyboards } from "@/keyboardLayouts";

// If we don't have a specific layout, default to "en-US" because that was the original layout
// developed so it is a good fallback. Additionally, we replace "en_US" with "en-US" because
// the original server-side code used "en_US" as the default value, but that's not the correct
// ISO code for English/United State. To ensure we remain backward compatible with devices that
// have not had their Keyboard Layout selected by the user, we want to treat "en_US" as if it was
// "en-US" to match the ISO standard codes now used in the keyboardLayouts.
export function isoCodeForLayout(keyboardLayout: string | undefined) {
  if (keyboardLayout && keyboardLayout.length > 0)
    return keyboardLayout.replace("en_US", "en-US");
  return "en-US";
}

// fallback to original behaviour of en-US if no isoCode given or matching layout not found
export function keyboardForLayout(keyboardLayout: string | undefined) {
  const isoCode = isoCodeForLayout(keyboardLayout);
  return (
    keyboards.find((keyboard) => keyboard.isoCode === isoCode) ??
    keyboards.find((keyboard) => keyboard.isoCode === "en-US")!
  );
}

export default function useKeyboardLayout() {
  const { keyboardLayout } = useSettingsStore();

  const keyboardOptions = useMemo(() => {
    return keyboards.map((keyboard) => {
      return { label: keyboard.name, value: keyboard.isoCode };
    });
  }, []);

  const isoCode = useMemo(() => {
    console.debug("Current keyboard layout from store:", keyboardLayout);
    return isoCodeForLayout(keyboardLayout);
  }, [keyboardLayout]);

  const selectedKeyboard = useMemo(
    () => keyboardForLayout(keyboardLayout),
    [keyboardLayout],
  );

  return { keyboardOptions, isoCode, selectedKeyboard };
}
