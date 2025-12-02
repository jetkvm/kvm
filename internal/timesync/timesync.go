package timesync

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/network/types"
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

type PreCheckFunc func() (bool, error)

type TimeSync struct {
	syncLock       *sync.Mutex
	loggingContext *logging.Context

	networkConfig    *types.NetworkConfig
	dhcpNtpAddresses []string

	rtcDevicePath string
	rtcDevice     *os.File //nolint:unused
	rtcLock       *sync.Mutex

	syncSuccess bool
	timer       *time.Timer

	preCheckFunc PreCheckFunc
	preCheckIPv4 PreCheckFunc
	preCheckIPv6 PreCheckFunc
}

type TimeSyncOptions struct {
	PreCheckFunc   PreCheckFunc
	PreCheckIPv4   PreCheckFunc
	PreCheckIPv6   PreCheckFunc
	LoggingContext *logging.Context
	NetworkConfig  *types.NetworkConfig
}

type SyncMode struct {
	Ntp             bool
	Http            bool
	Ordering        []string
	NtpUseFallback  bool
	HttpUseFallback bool
}

func NewTimeSync(opts *TimeSyncOptions) *TimeSync {
	loggingContext := opts.LoggingContext.With()
	rtcDevice, err := getRtcDevicePath()
	if err != nil {
		loggingContext.Error().Err(err).Msg("failed to get RTC device path")
	} else {
		loggingContext.Info().Str("path", rtcDevice).Msg("RTC device found")
	}

	t := &TimeSync{
		syncLock:         &sync.Mutex{},
		loggingContext:   loggingContext,
		dhcpNtpAddresses: []string{},
		rtcDevicePath:    rtcDevice,
		rtcLock:          &sync.Mutex{},
		preCheckFunc:     opts.PreCheckFunc,
		preCheckIPv4:     opts.PreCheckIPv4,
		preCheckIPv6:     opts.PreCheckIPv6,
		networkConfig:    opts.NetworkConfig,
		timer:            time.NewTimer(timeSyncWaitNetUpInt),
	}

	if t.rtcDevicePath != "" {
		rtcTime, _ := t.readRtcTime()
		loggingContext.Info().Interface("rtc_time", rtcTime).Msg("read RTC time")
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

	t.loggingContext.Debug().
		Strs("Ordering", syncMode.Ordering).
		Bool("Ntp", syncMode.Ntp).
		Bool("Http", syncMode.Http).
		Bool("NtpUseFallback", syncMode.NtpUseFallback).
		Bool("HttpUseFallback", syncMode.HttpUseFallback).
		Msg("sync mode")

	return syncMode
}
func (t *TimeSync) timeSyncLoop() {
	metricTimeSyncStatus.Set(0)

	for range t.timer.C {
		if ok, err := t.preCheckFunc(); !ok {
			if err != nil {
				t.loggingContext.Error().Err(err).Msg("pre-check failed")
			}
			t.timer.Reset(timeSyncWaitNetChkInt)
			continue
		}

		t.loggingContext.Info().Msg("syncing system time")
		start := time.Now()
		err := t.sync()
		if err != nil {
			t.loggingContext.Error().Str("error", err.Error()).Msg("failed to sync system time")

			// retry after a delay
			timeSyncRetryInterval += timeSyncRetryStep
			t.timer.Reset(timeSyncRetryInterval)
			// reset the retry interval if it exceeds the max interval
			if timeSyncRetryInterval > timeSyncRetryMaxInt {
				timeSyncRetryInterval = 0
			}
			continue
		}

		isInitialSync := !t.syncSuccess
		t.syncSuccess = true

		t.loggingContext.Info().Str("now", time.Now().Format(time.RFC3339)).
			Str("time_taken", time.Since(start).String()).
			Bool("is_initial_sync", isInitialSync).
			Msg("time sync successful")

		metricTimeSyncStatus.Set(1)

		t.timer.Reset(timeSyncInterval) // after the first sync is done
	}
}

func (t *TimeSync) sync() error {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	var (
		now    *time.Time
		offset *time.Duration
	)

	metricTimeSyncCount.Inc()

	syncMode := t.getSyncMode()
	loggingContext := t.loggingContext.With().Interface("sync_mode", syncMode)

Orders:
	for _, mode := range syncMode.Ordering {
		loopContext := loggingContext.With().Str("mode", mode)
		switch mode {
		case "ntp_user_provided":
			if syncMode.Ntp {
				loopContext.Info().Msg("using NTP custom servers")
				now, offset = t.queryNetworkTime(t.networkConfig.TimeSyncNTPServers)
				if now != nil {
					break Orders
				}
			}
		case "ntp_dhcp":
			if syncMode.Ntp {
				loopContext.Info().Msg("using NTP servers from DHCP")
				now, offset = t.queryNetworkTime(t.dhcpNtpAddresses)
				if now != nil {
					break Orders
				}
			}
		case "ntp":
			if syncMode.Ntp && syncMode.NtpUseFallback {
				loopContext.Info().Msg("using NTP fallback IPs")
				now, offset = t.queryNetworkTime(DefaultNTPServerIPs)
				if now == nil {
					loopContext.Info().Msg("using NTP fallback hostnames")
					now, offset = t.queryNetworkTime(DefaultNTPServerHostnames)
				}
				if now != nil {
					break Orders
				}
			}
		case "http_user_provided":
			if syncMode.Http {
				loopContext.Info().Msg("using HTTP custom URLs")
				now = t.queryAllHttpTime(t.networkConfig.TimeSyncHTTPUrls)
				if now != nil {
					break Orders
				}
			}
		case "http":
			if syncMode.Http && syncMode.HttpUseFallback {
				loopContext.Info().Msg("using HTTP fallback")
				now = t.queryAllHttpTime(defaultHTTPUrls)
				if now != nil {
					break Orders
				}
			}
		default:
			loopContext.Warn().Msg("unknown time sync mode, skipping")
		}
	}

	if now == nil {
		return fmt.Errorf("failed to get time from any source")
	}

	if offset != nil {
		newNow := time.Now().Add(*offset)
		now = &newNow
	}

	loggingContext.Info().Time("now", *now).Msg("time obtained")

	err := t.setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}

	metricTimeSyncSuccessCount.Inc()

	return nil
}

// Sync triggers a manual time sync
func (t *TimeSync) Sync() error {
	if !t.syncLock.TryLock() {
		t.loggingContext.With().Warn().Msg("sync already in progress, skipping")
		return nil
	}
	t.syncLock.Unlock()

	return t.sync()
}

// IsSyncSuccess returns true if the system time is synchronized
func (t *TimeSync) IsSyncSuccess() bool {
	return t.syncSuccess
}

// Start starts the time sync
func (t *TimeSync) Start() {
	go t.timeSyncLoop()
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
