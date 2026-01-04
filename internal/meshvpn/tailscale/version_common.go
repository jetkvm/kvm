package tailscale

import (
	"strconv"
	"strings"
)

// IsNewerVersion compares two semantic version strings and returns true if newVersion is newer.
// Pre-release suffixes are stripped before comparison, so "1.92.3" and "1.92.3-beta" are equal.
// Returns false if either version cannot be parsed.
func IsNewerVersion(currentVersion, newVersion string) bool {
	current, currentOk := parseVersion(currentVersion)
	newer, newerOk := parseVersion(newVersion)

	if !currentOk || !newerOk {
		return false
	}

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

// parseVersion parses a semantic version string into its numeric components.
// Returns the components and true on success, or nil and false if parsing fails.
func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		result[i] = n
	}
	return result, true
}
