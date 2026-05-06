export const isAndroidController = () =>
  typeof navigator !== "undefined" && /Android/i.test(navigator.userAgent);

const getPersistedFlag = (key: string, queryKey: string) => {
  if (typeof window === "undefined") return false;

  const queryValue = new URLSearchParams(window.location.search).get(queryKey);
  if (queryValue === "1" || queryValue === "0") {
    window.localStorage.setItem(key, queryValue);
    return queryValue === "1";
  }

  return window.localStorage.getItem(key) === "1";
};

const getPersistedFlagFromQueries = (key: string, queryKeys: string[]) => {
  if (typeof window === "undefined") return false;

  const params = new URLSearchParams(window.location.search);
  for (const queryKey of queryKeys) {
    const queryValue = params.get(queryKey);
    if (queryValue === "1" || queryValue === "0") {
      window.localStorage.setItem(key, queryValue);
      return queryValue === "1";
    }
  }

  return window.localStorage.getItem(key) === "1";
};

export const isAndroidControllerMode = () =>
  getPersistedFlag("androidControllerMode", "jetkvmAndroid");

export const isAndroidTouchscreenMode = () =>
  getPersistedFlagFromQueries("androidTouchscreen", ["androidTouchscreen", "jetkvmTouchscreen"]);

// Android controller mode is intentionally conservative: it is only enabled on
// Android browsers/WebViews when the controller explicitly opts in.
export const isAndroidCompactControllerMode = () =>
  typeof window !== "undefined" &&
  isAndroidControllerMode() &&
  isAndroidController() &&
  window.localStorage.getItem("androidCompactController") !== "0";
