package kvm

import (
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"github.com/beevik/ntp"
)

const (
	timeSyncRetryStep     = 5 * time.Second
	timeSyncRetryMaxInt   = 1 * time.Minute
	timeSyncWaitNetChkInt = 100 * time.Millisecond
	timeSyncWaitNetUpInt  = 3 * time.Second
	timeSyncInterval      = 1 * time.Hour
	timeSyncTimeout       = 2 * time.Second
)

var (
	builtTimestamp        string
	timeSyncRetryInterval = 0 * time.Second
	timeSyncSuccess       = false
	defaultNTPServers     = []string{
		"time.cloudflare.com",
		"time.apple.com",
	}
)

func isTimeSyncNeeded() bool {
	if builtTimestamp == "" {
		ntpLogger.Warn().Msg("Built timestamp is not set, time sync is needed")
		return true
	}

	ts, err := strconv.Atoi(builtTimestamp)
	if err != nil {
		ntpLogger.Warn().Str("error", err.Error()).Msg("Failed to parse built timestamp")
		return true
	}

	// builtTimestamp is UNIX timestamp in seconds
	builtTime := time.Unix(int64(ts), 0)
	now := time.Now()

	ntpLogger.Debug().Str("built_time", builtTime.Format(time.RFC3339)).Str("now", now.Format(time.RFC3339)).Msg("Built time and now")

	if now.Sub(builtTime) < 0 {
		ntpLogger.Warn().Msg("System time is behind the built time, time sync is needed")
		return true
	}

	return false
}

func TimeSyncLoop() {
	for {
		if !networkState.checked {
			time.Sleep(timeSyncWaitNetChkInt)
			continue
		}

		if !networkState.Up {
			ntpLogger.Info().Msg("Waiting for network to come up")
			time.Sleep(timeSyncWaitNetUpInt)
			continue
		}

		// check if time sync is needed, but do nothing for now
		isTimeSyncNeeded()

		ntpLogger.Info().Msg("Syncing system time")
		start := time.Now()
		err := SyncSystemTime()
		if err != nil {
			ntpLogger.Error().Str("error", err.Error()).Msg("Failed to sync system time")

			// retry after a delay
			timeSyncRetryInterval += timeSyncRetryStep
			time.Sleep(timeSyncRetryInterval)
			// reset the retry interval if it exceeds the max interval
			if timeSyncRetryInterval > timeSyncRetryMaxInt {
				timeSyncRetryInterval = 0
			}

			continue
		}
		timeSyncSuccess = true
		ntpLogger.Info().Str("now", time.Now().Format(time.RFC3339)).
			Str("time_taken", time.Since(start).String()).
			Msg("Time sync successful")
		time.Sleep(timeSyncInterval) // after the first sync is done
	}
}

func SyncSystemTime() (err error) {
	now, err := queryNetworkTime()
	if err != nil {
		return fmt.Errorf("failed to query network time: %w", err)
	}
	err = setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}
	return nil
}

func queryNetworkTime() (*time.Time, error) {
	ntpServers, err := getNTPServersFromDHCPInfo()
	if err != nil {
		ntpLogger.Info().Err(err).Msg("failed to get NTP servers from DHCP info")
	}

	if ntpServers == nil {
		ntpServers = defaultNTPServers
		ntpLogger.Info().
			Interface("ntp_servers", ntpServers).
			Msg("Using default NTP servers")
	} else {
		ntpLogger.Info().
			Interface("ntp_servers", ntpServers).
			Msg("Using NTP servers from DHCP")
	}

	for _, server := range ntpServers {
		now, err := queryNtpServer(server, timeSyncTimeout)
		if err == nil {
			ntpLogger.Info().
				Str("ntp_server", server).
				Str("time", now.Format(time.RFC3339)).
				Msg("NTP server returned time")
			return now, nil
		}
	}
	httpUrls := []string{
		"http://apple.com",
		"http://cloudflare.com",
	}
	for _, url := range httpUrls {
		now, err := queryHttpTime(url, timeSyncTimeout)
		if err == nil {
			ntpLogger.Info().
				Str("http_url", url).
				Str("time", now.Format(time.RFC3339)).
				Msg("HTTP server returned time")
			return now, nil
		}
	}

	return nil, ErrorfL(ntpLogger, "failed to query network time", nil)
}

func queryNtpServer(server string, timeout time.Duration) (now *time.Time, err error) {
	resp, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	return &resp.Time, nil
}

func queryHttpTime(url string, timeout time.Duration) (*time.Time, error) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Head(url)
	if err != nil {
		return nil, err
	}
	dateStr := resp.Header.Get("Date")
	now, err := time.Parse(time.RFC1123, dateStr)
	if err != nil {
		return nil, err
	}
	return &now, nil
}

func setSystemTime(now time.Time) error {
	nowStr := now.Format("2006-01-02 15:04:05")
	output, err := exec.Command("date", "-s", nowStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run date -s: %w, %s", err, string(output))
	}
	return nil
}
