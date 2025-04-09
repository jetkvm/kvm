import { useEffect } from "react";

import { useFeatureFlag } from "../hooks/useFeatureFlag";

export function FeatureFlag({
  minAppVersion,
  name = "unnamed",
  fallback = null,
  children,
}: {
  minAppVersion: string;
  name?: string;
  fallback?: React.ReactNode;
  children: React.ReactNode;
}) {
  const { isEnabled, appVersion } = useFeatureFlag(minAppVersion);

  useEffect(() => {
    if (!appVersion) return;
    console.log(
      `Feature '${name}' ${isEnabled ? "ENABLED" : "DISABLED"}: ` +
        `Current version: ${appVersion}, ` +
        `Required min version: ${minAppVersion || "N/A"}`,
    );
  }, [isEnabled, name, minAppVersion, appVersion]);

  return isEnabled ? children : fallback;
}
