package kvm

import (
	"strconv"
	"time"

	"github.com/jetkvm/kvm/internal/timesync"
)

var (
	timeSync          *timesync.TimeSync
	defaultNTPServers = []string{
		"time.apple.com",
		"time.aws.com",
		"time.windows.com",
		"time.google.com",
		"162.159.200.123", // time.cloudflare.com
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"3.pool.ntp.org",
	}
	defaultHTTPUrls = []string{
		"http://www.gstatic.com/generate_204",
		"http://cp.cloudflare.com/",
		"http://edge-http.microsoft.com/captiveportal/generate_204",
		// Firefox, Apple, and Microsoft have inconsistent results, so we don't use it
		// "http://detectportal.firefox.com/",
		// "http://www.apple.com/library/test/success.html",
		// "http://www.msftconnecttest.com/connecttest.txt",
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
