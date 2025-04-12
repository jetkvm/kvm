import { useContext } from "react";

import { FeatureFlagContext } from "@/providers/FeatureFlagContext";

export const useFeatureFlag = (minAppVersion: string) => {
  const context = useContext(FeatureFlagContext);

  if (!context) {
    throw new Error("useFeatureFlag must be used within a FeatureFlagProvider");
  }

  const { isFeatureEnabled, appVersion } = context;

  const isEnabled = isFeatureEnabled(minAppVersion);
  return { isEnabled, appVersion };
};
