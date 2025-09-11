import { useCallback, useEffect, useState } from "react";

const isOnDevice = import.meta.env.MODE === "device";
export function useReloadPage() {
  const [pageHash, setPageHash] = useState("");

  const fetchPageHash = useCallback(async () => {
    const response = await fetch("/static-hash.json");
    const data = await response.json();
    setPageHash(data.hash);
  }, [setPageHash]);

  useEffect(() => {
    if (!isOnDevice) return ;

    const interval = setInterval(() => fetchPageHash(), 1000);
    return () => clearInterval(interval);
  }, [fetchPageHash]);

  useEffect(() => {
    if (!isOnDevice) return ;

  }, [pageHash]);
}