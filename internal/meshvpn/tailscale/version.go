//go:build linux

package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

type Track string

const (
	TrackStable   Track = "stable"
	TrackUnstable Track = "unstable"

	VersionAPIURL        = "https://pkgs.tailscale.com"
	VersionCacheDuration = 1 * time.Hour
)

type VersionInfo struct {
	Version         string            `json:"Version"`
	TarballsVersion string            `json:"TarballsVersion"`
	Tarballs        map[string]string `json:"Tarballs"`
	Track           Track             `json:"-"`
	FetchedAt       time.Time         `json:"-"`
}

// VersionClient fetches and caches version information from Tailscale's API.
type VersionClient struct {
	httpClient meshvpn.HTTPClient
	mu         sync.RWMutex
	cache      map[Track]*VersionInfo
}

func NewVersionClient(httpClient meshvpn.HTTPClient) *VersionClient {
	return &VersionClient{
		httpClient: httpClient,
		cache:      make(map[Track]*VersionInfo),
	}
}

func (c *VersionClient) GetLatestVersion(ctx context.Context, track Track) (*VersionInfo, error) {
	c.mu.RLock()
	if cached, ok := c.cache[track]; ok {
		if time.Since(cached.FetchedAt) < VersionCacheDuration {
			c.mu.RUnlock()
			return cached, nil
		}
	}
	c.mu.RUnlock()

	url := fmt.Sprintf("%s/%s/?mode=json", VersionAPIURL, track)

	data, err := c.httpClient.Get(url)
	if err != nil {
		c.mu.RLock()
		cached, ok := c.cache[track]
		c.mu.RUnlock()
		if ok {
			logger.Warn().Err(err).Msg("failed to fetch latest version, using cached")
			return cached, nil
		}
		return nil, fmt.Errorf("failed to fetch version info: %w", err)
	}

	var info VersionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse version info: %w", err)
	}

	info.Track = track
	info.FetchedAt = time.Now()

	c.mu.Lock()
	c.cache[track] = &info
	c.mu.Unlock()

	return &info, nil
}
