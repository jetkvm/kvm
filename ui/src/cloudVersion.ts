import semver from "semver";

/**
 * Select the versioned cloud UI bundle for a device. Devices at or above the
 * compatibility floor get the bundle built for their own version; anything
 * older, unparseable or unknown gets the floor bundle. A JetKVM Mini's bundle
 * is published for every firmware version as <version>-mini, which SemVer
 * reads as a prerelease below the floor, so it is used as it is.
 */
export function selectCloudUiVersion(
  appVersion: string | undefined,
  backwardsCompatibleVersion: string,
): string {
  if (appVersion?.endsWith("-mini")) return appVersion;
  return appVersion &&
    semver.valid(appVersion) &&
    semver.gte(appVersion, backwardsCompatibleVersion)
    ? appVersion
    : backwardsCompatibleVersion;
}
