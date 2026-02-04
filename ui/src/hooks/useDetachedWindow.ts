import { isOnDevice } from "@/main";
import { useSettingsStore } from "@hooks/stores";

// Module-level Map to track windows (avoids serialization issues)
const windowMap = new Map<string, Window>();

// Height constants for window sizing
const VIDEO_HEIGHT = 720;
const DETACHED_TOOLBAR_HEIGHT = 32; // h-8 = 32px
const ACTION_BAR_HEIGHT = 40; // min-h-[39.5px] ≈ 40px
const INFO_BAR_HEIGHT = 24; // approximately 24px

export function useDetachedWindow() {
  const { showDetachedToolbar } = useSettingsStore();

  const openDetachedWindow = (deviceId: string) => {
    // Check existing window
    const existing = windowMap.get(deviceId);
    if (existing && !existing.closed) {
      existing.focus();
      return;
    }

    // Calculate window height based on toolbar visibility
    // When toolbar is shown: video + detached toolbar + action bar + info bar
    // When toolbar is hidden: video only (clean view)
    const width = 1280;
    const extraHeight = showDetachedToolbar
      ? DETACHED_TOOLBAR_HEIGHT + ACTION_BAR_HEIGHT + INFO_BAR_HEIGHT
      : 0;
    const height = VIDEO_HEIGHT + extraHeight;

    const left = Math.max(0, (window.screen.width - width) / 2);
    const top = Math.max(0, (window.screen.height - height) / 2);
    const features = `width=${width},height=${height},left=${left},top=${top},menubar=no,toolbar=no,location=no,status=no,resizable=yes`;
    const url = isOnDevice ? "/detached" : `/devices/${deviceId}/detached`;

    const win = window.open(url, `jetkvm-${deviceId}`, features);
    if (win) {
      win.document.title = "JetKVM";
      windowMap.set(deviceId, win);
      // Cleanup on close
      const interval = setInterval(() => {
        if (win.closed) {
          windowMap.delete(deviceId);
          clearInterval(interval);
        }
      }, 1000);
    }
  };

  return { openDetachedWindow };
}
