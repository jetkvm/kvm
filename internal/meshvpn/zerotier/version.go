//go:build linux

package zerotier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
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
	httpClient meshvpn.HTTPClient
	mu         sync.RWMutex
	cache      *VersionInfo
}

func NewVersionClient(httpClient meshvpn.HTTPClient) *VersionClient {
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
