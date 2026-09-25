import { useSyncExternalStore } from "react";

const getDevicePixelRatio = () => window.devicePixelRatio;

// A resolution media query only matches the current ratio, so it is re-armed
// after every change (browser zoom, or the window moving to another display).
function subscribe(onChange: () => void) {
  let query: MediaQueryList;
  function arm() {
    query = window.matchMedia(`(resolution: ${getDevicePixelRatio()}dppx)`);
    query.addEventListener("change", handleChange, { once: true });
  }
  function handleChange() {
    arm();
    onChange();
  }
  arm();
  return () => query.removeEventListener("change", handleChange);
}

export function useDevicePixelRatio(): number {
  return useSyncExternalStore(subscribe, getDevicePixelRatio);
}
