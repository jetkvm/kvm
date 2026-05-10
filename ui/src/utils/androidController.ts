declare global {
  interface Window {
    JetKVMAndroid?: unknown;
  }
}

export const isAndroidControllerApk = () => {
  if (typeof window === "undefined") return false;

  const params = new URLSearchParams(window.location.search);
  return params.get("jetkvmAndroid") === "1" || typeof window.JetKVMAndroid !== "undefined";
};

export const isAndroidCompactControllerMode = () => {
  if (typeof window === "undefined") return false;

  return isAndroidControllerApk();
};
