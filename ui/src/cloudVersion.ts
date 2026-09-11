import semver from "semver";

/**
 * Select the versioned cloud UI bundle for a device. Devices at or above the
 * compatibility floor get the bundle built for their own version; anything
 * older, unparseable or unknown gets the floor bundle.
 */
export function selectCloudUiVersion(
  appVersion: string | undefined,
  backwardsCompatibleVersion: string,
): string {
  return appVersion &&
    semver.valid(appVersion) &&
    semver.gte(appVersion, backwardsCompatibleVersion)
    ? appVersion
    : backwardsCompatibleVersion;
}
