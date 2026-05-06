export const isAndroidController = () =>
  typeof navigator !== "undefined" && /Android/i.test(navigator.userAgent);

export const isAndroidCompactControllerMode = () => {
  if (typeof window === "undefined") return false;

  const setting = window.localStorage.getItem("androidCompactController");
  if (setting === "0") return false;
  if (setting === "1") return true;

  return isAndroidController();
};
