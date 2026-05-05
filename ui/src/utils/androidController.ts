export const isAndroidTouchscreenMode = () =>
  typeof window !== "undefined" && window.localStorage.getItem("androidTouchscreen") !== "0";

export const isAndroidController = () =>
  typeof navigator !== "undefined" && /Android/i.test(navigator.userAgent);

export const isAndroidCompactControllerMode = () =>
  typeof window !== "undefined" &&
  isAndroidTouchscreenMode() &&
  isAndroidController() &&
  window.localStorage.getItem("androidCompactController") !== "0";
