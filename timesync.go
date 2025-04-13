package kvm

import (
	"strconv"
	"time"

	"github.com/jetkvm/kvm/internal/timesync"
)

var (
	timeSync          *timesync.TimeSync
	defaultNTPServers = []string{
		"time.cloudflare.com",
		"time.apple.com",
	}
	defaultHTTPUrls = []string{
		"http://apple.com",
		"http://cloudflare.com",
	}
	builtTimestamp string
)

func isTimeSyncNeeded() bool {
	if builtTimestamp == "" {
		timesyncLogger.Warn().Msg("built timestamp is not set, time sync is needed")
		return true
	}

	ts, err := strconv.Atoi(builtTimestamp)
	if err != nil {
		timesyncLogger.Warn().Str("error", err.Error()).Msg("failed to parse built timestamp")
		return true
	}

	// builtTimestamp is UNIX timestamp in seconds
	builtTime := time.Unix(int64(ts), 0)
	now := time.Now()

	if now.Sub(builtTime) < 0 {
		timesyncLogger.Warn().
			Str("built_time", builtTime.Format(time.RFC3339)).
			Str("now", now.Format(time.RFC3339)).
			Msg("system time is behind the built time, time sync is needed")
		return true
	}

	return false
}

func initTimeSync() {
	timeSync = timesync.NewTimeSync(
		func() (bool, error) {
			if !networkState.IsOnline() {
				return false, nil
			}
			return true, nil
		},
		defaultNTPServers,
		defaultHTTPUrls,
		timesyncLogger,
	)
}
