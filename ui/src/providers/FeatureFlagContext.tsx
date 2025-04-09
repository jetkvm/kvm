import { createContext } from "react";

import { FeatureFlagContextType } from "./FeatureFlagProvider";

// Create the context

export const FeatureFlagContext = createContext<FeatureFlagContextType>({
  appVersion: null,
  isFeatureEnabled: () => false,
});
