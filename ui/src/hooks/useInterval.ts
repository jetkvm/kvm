import { useEffect, useRef } from "react";

export default function useInterval(callback: () => void, delay: number) {
  const savedCallback = useRef<typeof callback>();

  // Save the callback directly in the useRef object
  savedCallback.current = callback;

  // Set up the interval.
  useEffect(() => {
    function tick() {
      if (!savedCallback.current) return;
      savedCallback.current();
    }

    if (delay !== null) {
      const id = setInterval(tick, delay);
      return () => clearInterval(id);
    }
  }, [delay]);
}
