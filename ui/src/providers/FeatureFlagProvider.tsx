import semver from "semver";

import { FeatureFlagContext } from "./FeatureFlagContext";

export interface FeatureFlagContextType {
  appVersion: string | null;
  isFeatureEnabled: (minVersion: string) => boolean;
}

// Provider component that fetches version and provides context
export const FeatureFlagProvider = ({
  children,
  appVersion,
}: {
  children: React.ReactNode;
  appVersion: string | null;
}) => {
  const isFeatureEnabled = (minAppVersion: string) => {
    // If no version is set, feature is disabled.
    // The feature flag component can decide what to display as a fallback - either omit the component or like a "please upgrade to enable".
    if (!appVersion) return false;

    // Extract the base versions without prerelease identifier
    const baseCurrentVersion = semver.coerce(appVersion)?.version;
    const baseMinVersion = semver.coerce(minAppVersion)?.version;

    if (!baseCurrentVersion || !baseMinVersion) return false;

    return semver.gte(baseCurrentVersion, baseMinVersion);
  };

  const value = { appVersion, isFeatureEnabled };

  return (
    <FeatureFlagContext.Provider value={value}>{children}</FeatureFlagContext.Provider>
  );
};
