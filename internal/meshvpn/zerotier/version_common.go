package zerotier

import (
	"regexp"
	"strconv"
)

// IsNewerVersion compares two semantic versions and returns true if latest > current.
// Only supports versions with exactly 3 numeric parts (MAJOR.MINOR.PATCH).
// Returns false if either version cannot be parsed.
func IsNewerVersion(current, latest string) bool {
	re := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

	currentParts := re.FindStringSubmatch(current)
	latestParts := re.FindStringSubmatch(latest)

	if len(currentParts) < 4 || len(latestParts) < 4 {
		return false
	}

	for i := 1; i <= 3; i++ {
		c, err := strconv.Atoi(currentParts[i])
		if err != nil {
			return false
		}
		l, err := strconv.Atoi(latestParts[i])
		if err != nil {
			return false
		}
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}

	return false
}
