import { useEffect, useState } from "react";

import { useVideoStreamPause } from "@hooks/useVideoStreamPause";

export function useHiddenVideoStreamPause(): void {
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    let timeout: ReturnType<typeof setTimeout> | undefined;

    const updateVisibility = () => {
      clearTimeout(timeout);
      if (document.visibilityState === "hidden") {
        timeout = setTimeout(() => setPaused(true), 5_000);
      } else {
        setPaused(false);
      }
    };

    document.addEventListener("visibilitychange", updateVisibility);
    updateVisibility();

    return () => {
      document.removeEventListener("visibilitychange", updateVisibility);
      clearTimeout(timeout);
    };
  }, []);

  useVideoStreamPause(paused);
}
