package timesync

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/network"
	"github.com/rs/zerolog"
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
	timeSyncRetryInterval = 0 * time.Second
)

type TimeSync struct {
	syncLock *sync.Mutex
	l        *zerolog.Logger

	networkConfig    *network.NetworkConfig
	dhcpNtpAddresses []string

	rtcDevicePath string
	rtcDevice     *os.File //nolint:unused
	rtcLock       *sync.Mutex

	syncSuccess bool

	preCheckFunc func() (bool, error)
}

type TimeSyncOptions struct {
	PreCheckFunc  func() (bool, error)
	Logger        *zerolog.Logger
	NetworkConfig *network.NetworkConfig
}

type SyncMode struct {
	Ntp             bool
	Http            bool
	Ordering        []string
	NtpUseFallback  bool
	HttpUseFallback bool
}

func NewTimeSync(opts *TimeSyncOptions) *TimeSync {
	rtcDevice, err := getRtcDevicePath()
	if err != nil {
		opts.Logger.Error().Err(err).Msg("failed to get RTC device path")
	} else {
		opts.Logger.Info().Str("path", rtcDevice).Msg("RTC device found")
	}

	t := &TimeSync{
		syncLock:         &sync.Mutex{},
		l:                opts.Logger,
		dhcpNtpAddresses: []string{},
		rtcDevicePath:    rtcDevice,
		rtcLock:          &sync.Mutex{},
		preCheckFunc:     opts.PreCheckFunc,
		networkConfig:    opts.NetworkConfig,
	}

	if t.rtcDevicePath != "" {
		rtcTime, _ := t.readRtcTime()
		t.l.Info().Interface("rtc_time", rtcTime).Msg("read RTC time")
	}

	return t
}

func (t *TimeSync) SetDhcpNtpAddresses(addresses []string) {
	t.dhcpNtpAddresses = addresses
}

func (t *TimeSync) getSyncMode() SyncMode {
	syncMode := SyncMode{
		Ntp:             true,
		Http:            true,
		Ordering:        []string{"ntp_dhcp", "ntp", "http"},
		NtpUseFallback:  true,
		HttpUseFallback: true,
	}

	if t.networkConfig != nil {
		switch t.networkConfig.TimeSyncMode.String {
		case "ntp_only":
			syncMode.Http = false
		case "http_only":
			syncMode.Ntp = false
		}

		if t.networkConfig.TimeSyncDisableFallback.Bool {
			syncMode.NtpUseFallback = false
			syncMode.HttpUseFallback = false
		}

		var syncOrdering = t.networkConfig.TimeSyncOrdering
		if len(syncOrdering) > 0 {
			syncMode.Ordering = syncOrdering
		}
	}

	t.l.Debug().Strs("Ordering", syncMode.Ordering).Bool("Ntp", syncMode.Ntp).Bool("Http", syncMode.Http).Bool("NtpUseFallback", syncMode.NtpUseFallback).Bool("HttpUseFallback", syncMode.HttpUseFallback).Msg("sync mode")

	return syncMode
}
func (t *TimeSync) doTimeSync() {
	metricTimeSyncStatus.Set(0)
	for {
		if ok, err := t.preCheckFunc(); !ok {
			if err != nil {
				t.l.Error().Err(err).Msg("pre-check failed")
			}
			time.Sleep(timeSyncWaitNetChkInt)
			continue
		}

		t.l.Info().Msg("syncing system time")
		start := time.Now()
		err := t.Sync()
		if err != nil {
			t.l.Error().Str("error", err.Error()).Msg("failed to sync system time")

			// retry after a delay
			timeSyncRetryInterval += timeSyncRetryStep
			time.Sleep(timeSyncRetryInterval)
			// reset the retry interval if it exceeds the max interval
			if timeSyncRetryInterval > timeSyncRetryMaxInt {
				timeSyncRetryInterval = 0
			}

			continue
		}
		t.syncSuccess = true
		t.l.Info().Str("now", time.Now().Format(time.RFC3339)).
			Str("time_taken", time.Since(start).String()).
			Msg("time sync successful")

		metricTimeSyncStatus.Set(1)

		time.Sleep(timeSyncInterval) // after the first sync is done
	}
}

func (t *TimeSync) Sync() error {
	var (
		now    *time.Time
		offset *time.Duration
	)

	metricTimeSyncCount.Inc()

	syncMode := t.getSyncMode()

Orders:
	for _, mode := range syncMode.Ordering {
		switch mode {
		case "ntp_user_provided":
			if syncMode.Ntp {
				t.l.Info().Msg("using NTP custom servers")
				now, offset = t.queryNetworkTime(t.networkConfig.TimeSyncNTPServers)
				if now != nil {
					t.l.Info().Str("source", "NTP").Time("now", *now).Msg("time obtained")
					break Orders
				}
			}
		case "ntp_dhcp":
			if syncMode.Ntp {
				t.l.Info().Msg("using NTP servers from DHCP")
				now, offset = t.queryNetworkTime(t.dhcpNtpAddresses)
				if now != nil {
					t.l.Info().Str("source", "NTP DHCP").Time("now", *now).Msg("time obtained")
					break Orders
				}
			}
		case "ntp":
			if syncMode.Ntp && syncMode.NtpUseFallback {
				t.l.Info().Msg("using NTP fallback")
				now, offset = t.queryNetworkTime(defaultNTPServers)
				if now != nil {
					t.l.Info().Str("source", "NTP fallback").Time("now", *now).Msg("time obtained")
					break Orders
				}
			}
		case "http_user_provided":
			if syncMode.Http {
				t.l.Info().Msg("using HTTP custom URLs")
				now = t.queryAllHttpTime(t.networkConfig.TimeSyncHTTPUrls)
				if now != nil {
					t.l.Info().Str("source", "HTTP").Time("now", *now).Msg("time obtained")
					break Orders
				}
			}
		case "http":
			if syncMode.Http && syncMode.HttpUseFallback {
				t.l.Info().Msg("using HTTP fallback")
				now = t.queryAllHttpTime(defaultHTTPUrls)
				if now != nil {
					t.l.Info().Str("source", "HTTP fallback").Time("now", *now).Msg("time obtained")
					break Orders
				}
			}
		default:
			t.l.Warn().Str("mode", mode).Msg("unknown time sync mode, skipping")
		}
	}

	if now == nil {
		return fmt.Errorf("failed to get time from any source")
	}

	if offset != nil {
		newNow := time.Now().Add(*offset)
		now = &newNow
	}

	err := t.setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}

	metricTimeSyncSuccessCount.Inc()

	return nil
}

func (t *TimeSync) IsSyncSuccess() bool {
	return t.syncSuccess
}

func (t *TimeSync) Start() {
	go t.doTimeSync()
}

func (t *TimeSync) setSystemTime(now time.Time) error {
	nowStr := now.Format("2006-01-02 15:04:05")
	output, err := exec.Command("date", "-s", nowStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run date -s: %w, %s", err, string(output))
	}

	if t.rtcDevicePath != "" {
		return t.setRtcTime(now)
	}

	return nil
}
