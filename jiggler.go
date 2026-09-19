package kvm

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/go-co-op/gocron/v2"
	"github.com/jetkvm/kvm/internal/tzdata"
)

type JigglerConfig struct {
	InactivityLimitSeconds int    `json:"inactivity_limit_seconds"`
	JitterPercentage       int    `json:"jitter_percentage"`
	ScheduleCronTab        string `json:"schedule_cron_tab"`
	Timezone               string `json:"timezone,omitempty"`
}

var jobDelta time.Duration = 0
var scheduler gocron.Scheduler = nil

// schedulerLock serialises scheduler replacement: initJiggler may still be
// waiting for a trustworthy clock when a config change arrives.
var schedulerLock sync.Mutex

func rpcSetJigglerState(enabled bool) error {
	config.JigglerEnabled = enabled
	err := SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if mqttManager != nil {
		mqttManager.publishJigglerState()
	}
	return nil
}

func rpcGetJigglerState() bool {
	return config.JigglerEnabled
}

func rpcGetTimezones() []string {
	return tzdata.TimeZones
}

func rpcGetJigglerConfig() (JigglerConfig, error) {
	return *config.JigglerConfig, nil
}

func rpcSetJigglerConfig(jigglerConfig JigglerConfig) error {
	logger.Info().Msgf("jigglerConfig: %v, %v, %v, %v", jigglerConfig.InactivityLimitSeconds, jigglerConfig.JitterPercentage, jigglerConfig.ScheduleCronTab, jigglerConfig.Timezone)
	config.JigglerConfig = &jigglerConfig
	schedulerLock.Lock()
	err := replaceJigglerCronTabLocked()
	schedulerLock.Unlock()
	if err != nil {
		return err
	}
	err = SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func removeExistingCrobJobs(s gocron.Scheduler) error {
	// No scheduler yet while initJiggler waits for a trustworthy clock.
	if s == nil {
		return nil
	}
	for _, j := range s.Jobs() {
		err := s.RemoveJob(j.ID())
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	jigglerClockPollInterval = 5 * time.Second
	jigglerClockWaitLimit    = 10 * time.Minute
)

func initJiggler() {
	ensureConfigLoaded()

	// A cold boot starts at the UNIX epoch (no battery-backed RTC) and time
	// sync later jumps the clock forward by decades. gocron reacts to a
	// next-run in the past by walking the cron expression forward one step at
	// a time: ~30M steps from 1970 for a per-minute schedule, over an hour of
	// pegged CPU with no jiggles, and it cannot be interrupted once started.
	// So don't build the schedule until the clock is trustworthy.
	if !isTimeSyncNeeded() {
		startJigglerCronTab()
		return
	}

	logger.Info().Msg("system clock is not yet trustworthy, deferring jiggler schedule until time sync")
	go func() {
		if waitForTrustworthyClock(timeSyncSucceeded, jigglerClockPollInterval, jigglerClockWaitLimit) {
			logger.Info().Msg("clock synced, scheduling jiggler")
		} else {
			// An offline device with no jiggler at all is the worse failure.
			logger.Warn().Msgf("clock still unsynced after %v, scheduling jiggler anyway", jigglerClockWaitLimit)
		}
		startJigglerCronTab()
	}()
}

// timeSyncSucceeded is the quiet form of the clock check: isTimeSyncNeeded logs
// a warning on every call, which a poll loop would turn into log spam.
func timeSyncSucceeded() bool {
	return timeSync != nil && timeSync.IsSyncSuccess()
}

// waitForTrustworthyClock polls until synced reports true, reporting whether it
// did so within the limit. Time sync offers no completion signal to subscribe
// to. The elapsed check is monotonic, so the jump being waited on cannot skew it.
func waitForTrustworthyClock(synced func() bool, poll time.Duration, limit time.Duration) bool {
	start := time.Now()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		if synced() {
			return true
		}
		if time.Since(start) >= limit {
			return false
		}
		<-ticker.C
	}
}

func startJigglerCronTab() {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if err := replaceJigglerCronTabLocked(); err != nil {
		logger.Error().Err(err).Msg("error scheduling jiggler crontab")
	}
}

// replaceJigglerCronTabLocked rebuilds the schedule. Caller must hold schedulerLock.
func replaceJigglerCronTabLocked() error {
	if err := removeExistingCrobJobs(scheduler); err != nil {
		return fmt.Errorf("error removing cron jobs from scheduler: %w", err)
	}
	if err := runJigglerCronTab(); err != nil {
		return fmt.Errorf("error scheduling jiggler crontab: %w", err)
	}
	return nil
}

func runJigglerCronTab() error {
	cronTab := config.JigglerConfig.ScheduleCronTab

	// Apply timezone if specified and valid
	if config.JigglerConfig.Timezone != "" && config.JigglerConfig.Timezone != "UTC" {
		// Validate timezone before applying
		if _, err := time.LoadLocation(config.JigglerConfig.Timezone); err != nil {
			logger.Warn().Msgf("Invalid timezone '%s', falling back to UTC: %v", config.JigglerConfig.Timezone, err)
			// Don't add TZ prefix, let it run in UTC
		} else {
			cronTab = fmt.Sprintf("TZ=%s %s", config.JigglerConfig.Timezone, cronTab)
		}
	}

	s, err := gocron.NewScheduler()
	if err != nil {
		return err
	}
	scheduler = s
	_, err = s.NewJob(
		gocron.CronJob(
			cronTab,
			true,
		),
		gocron.NewTask(
			func() {
				runJiggler()
			},
		),
	)
	if err != nil {
		return err
	}
	s.Start()
	delta, err := calculateJobDelta(s)
	jobDelta = delta
	logger.Info().Msgf("Time between jiggler runs: %v", jobDelta)
	if err != nil {
		return err
	}
	return nil
}

func runJiggler() {
	if config.JigglerEnabled {
		if config.JigglerConfig.JitterPercentage != 0 {
			jitter := calculateJitterDuration(jobDelta)
			time.Sleep(jitter)
		}
		inactivitySeconds := config.JigglerConfig.InactivityLimitSeconds
		timeSinceLastInput := time.Since(gadget.GetLastUserInputTime())
		logger.Debug().Msgf("Time since last user input %v", timeSinceLastInput)
		if timeSinceLastInput > time.Duration(inactivitySeconds)*time.Second {
			logger.Debug().Msg("Jiggling mouse...")
			dx := int8(rand.Intn(3) + 1)
			dy := int8(rand.Intn(3) + 1)
			if rand.Intn(2) == 0 {
				dx = -dx
			}
			if rand.Intn(2) == 0 {
				dy = -dy
			}
			err := rpcRelMouseReport(dx, dy, 0)
			if err != nil {
				logger.Warn().Msgf("Failed to jiggle mouse: %v", err)
			}
		}
	}
}

func calculateJobDelta(s gocron.Scheduler) (time.Duration, error) {
	j := s.Jobs()[0]
	runs, err := j.NextRuns(2)
	if err != nil {
		return 0.0, err
	}
	return runs[1].Sub(runs[0]), nil
}

func calculateJitterDuration(delta time.Duration) time.Duration {
	jitter := rand.Float64() * float64(config.JigglerConfig.JitterPercentage) / 100 * delta.Seconds()
	return time.Duration(jitter * float64(time.Second))
}
