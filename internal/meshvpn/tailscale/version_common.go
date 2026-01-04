package tailscale

import (
	"strconv"
	"strings"
)

// IsNewerVersion compares two semantic version strings and returns true if newVersion is newer.
// Pre-release suffixes are stripped before comparison, so "1.92.3" and "1.92.3-beta" are equal.
func IsNewerVersion(currentVersion, newVersion string) bool {
	current := parseVersion(currentVersion)
	newer := parseVersion(newVersion)

	for i := 0; i < len(current) && i < len(newer); i++ {
		if newer[i] > current[i] {
			return true
		}
		if newer[i] < current[i] {
			return false
		}
	}
	return len(newer) > len(current)
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	// Strip pre-release suffix (e.g., "1.92.3-beta" -> "1.92.3")
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		result[i], _ = strconv.Atoi(p)
	}
	return result
}
