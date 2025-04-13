package timesync

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

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

	ntpServers []string
	httpUrls   []string

	rtcDevicePath string
	rtcDevice     *os.File
	rtcLock       *sync.Mutex

	syncSuccess bool

	preCheckFunc func() (bool, error)
}

func NewTimeSync(
	precheckFunc func() (bool, error),
	ntpServers []string,
	httpUrls []string,
	logger *zerolog.Logger,
) *TimeSync {
	rtcDevice, err := getRtcDevicePath()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get RTC device path")
	} else {
		logger.Info().Str("path", rtcDevice).Msg("RTC device found")
	}

	t := &TimeSync{
		syncLock:      &sync.Mutex{},
		l:             logger,
		rtcDevicePath: rtcDevice,
		rtcLock:       &sync.Mutex{},
		preCheckFunc:  precheckFunc,
		ntpServers:    ntpServers,
		httpUrls:      httpUrls,
	}

	if t.rtcDevicePath != "" {
		rtcTime, _ := t.readRtcTime()
		t.l.Info().Interface("rtc_time", rtcTime).Msg("read RTC time")
	}

	return t
}

func (t *TimeSync) doTimeSync() {
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

		time.Sleep(timeSyncInterval) // after the first sync is done
	}
}

func (t *TimeSync) Sync() error {
	var now *time.Time
	now = t.queryNetworkTime()
	if now == nil {
		now = t.queryAllHttpTime()
	}

	if now == nil {
		return fmt.Errorf("failed to get time from any source")
	}

	err := t.setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}

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
