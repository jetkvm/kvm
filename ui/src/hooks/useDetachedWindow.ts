import { isOnDevice } from "@/main";

// Module-level Map to track windows (avoids serialization issues)
const windowMap = new Map<string, Window>();

export function useDetachedWindow() {
  const openDetachedWindow = (deviceId: string) => {
    // Check existing window
    const existing = windowMap.get(deviceId);
    if (existing && !existing.closed) {
      existing.focus();
      return;
    }

    const width = 1280;
    const height = 720;
    const left = Math.max(0, (window.screen.width - width) / 2);
    const top = Math.max(0, (window.screen.height - height) / 2);
    const features = `width=${width},height=${height},left=${left},top=${top},menubar=no,toolbar=no,location=no,status=no,resizable=yes`;
    const url = isOnDevice ? "/?detached=true" : `/devices/${deviceId}?detached=true`;

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
