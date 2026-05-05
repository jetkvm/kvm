export const isAndroidTouchscreenMode = () =>
  typeof window !== "undefined" && window.localStorage.getItem("androidTouchscreen") !== "0";

export const isAndroidController = () =>
  typeof navigator !== "undefined" && /Android/i.test(navigator.userAgent);

// Android controller mode is intentionally conservative: it is only enabled on
// Android browsers/WebViews when the Android touchscreen HID path is enabled.
export const isAndroidCompactControllerMode = () =>
  typeof window !== "undefined" &&
  isAndroidTouchscreenMode() &&
  isAndroidController() &&
  window.localStorage.getItem("androidCompactController") !== "0";
