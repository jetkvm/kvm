//go:build linux

package zerotier

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// GitHub API for releases
	GitHubReleasesAPI    = "https://api.github.com/repos/zerotier/ZeroTierOne/releases/latest"
	VersionCacheDuration = 1 * time.Hour
)

// VersionInfo holds version information for updates.
type VersionInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	DownloadURL     string `json:"downloadUrl,omitempty"`
	FetchedAt       time.Time
}

// GitHubRelease represents a GitHub release API response.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// VersionClient fetches and caches version information.
type VersionClient struct {
	httpClient HTTPClient
	mu         sync.RWMutex
	cache      *VersionInfo
}

func NewVersionClient(httpClient HTTPClient) *VersionClient {
	return &VersionClient{
		httpClient: httpClient,
	}
}

func (c *VersionClient) GetLatestVersion(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.cache != nil && time.Since(c.cache.FetchedAt) < VersionCacheDuration {
		version := c.cache.LatestVersion
		c.mu.RUnlock()
		return version, nil
	}
	c.mu.RUnlock()

	data, err := c.httpClient.Get(GitHubReleasesAPI)
	if err != nil {
		c.mu.RLock()
		if c.cache != nil {
			version := c.cache.LatestVersion
			c.mu.RUnlock()
			logger.Warn().Err(err).Msg("failed to fetch latest version, using cached")
			return version, nil
		}
		c.mu.RUnlock()
		return "", fmt.Errorf("failed to fetch version info: %w", err)
	}

	var release GitHubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("failed to parse version info: %w", err)
	}

	// Extract version from tag (e.g., "1.16.0" from "1.16.0" or "v1.16.0")
	version := strings.TrimPrefix(release.TagName, "v")

	c.mu.Lock()
	c.cache = &VersionInfo{
		LatestVersion: version,
		FetchedAt:     time.Now(),
	}
	c.mu.Unlock()

	return version, nil
}

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
