export const isAndroidController = () =>
  typeof navigator !== "undefined" && /Android/i.test(navigator.userAgent);

export const isAndroidTouchscreenMode = () => {
  if (typeof window === "undefined") return false;

  const touchscreenMode = window.localStorage.getItem("androidTouchscreen");
  if (touchscreenMode === "1") return true;
  if (touchscreenMode === "0") return false;

  return new URLSearchParams(window.location.search).get("jetkvmAndroid") === "1";
};

// Android controller mode is intentionally conservative: it is only enabled on
// Android browsers/WebViews when the Android touchscreen HID path is enabled.
export const isAndroidCompactControllerMode = () =>
  typeof window !== "undefined" &&
  isAndroidTouchscreenMode() &&
  isAndroidController() &&
  window.localStorage.getItem("androidCompactController") !== "0";
