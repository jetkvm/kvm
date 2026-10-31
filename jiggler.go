package kvm

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/go-co-op/gocron/v2"
	"github.com/jetkvm/kvm/internal/timesync"
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

// schedulerLock serialises scheduler replacement, and guards the flag below.
var schedulerLock sync.Mutex

// jigglerSchedulePending is set between time sync announcing a clock jump and
// completing it. A config change arriving in that window must not build the
// schedule itself, or it would put it on the clock that is about to change.
var jigglerSchedulePending bool

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
	if err := rebuildJigglerCronTab(); err != nil {
		return err
	}
	err := SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// shutdownJigglerSchedulerLocked stops the current scheduler, if there is one.
// Caller must hold schedulerLock. gocron cancels the scheduler before it waits
// for a running job, so a job finishing afterwards is not rescheduled: that is
// what makes stopping ahead of a clock jump safe. The wait is bounded by
// gocron's stop timeout, so this cannot hang on a catch-up walk either.
func shutdownJigglerSchedulerLocked() {
	if scheduler == nil {
		return
	}
	if err := scheduler.Shutdown(); err != nil {
		logger.Warn().Err(err).Msg("jiggler scheduler did not stop cleanly")
	}
	scheduler = nil
}

// jigglerClockJumpLimit is the smallest clock change worth rebuilding the
// schedule for. The hourly NTP corrections are milliseconds and gocron absorbs
// them; anything larger costs it a catch-up step per schedule interval skipped.
const jigglerClockJumpLimit = time.Minute

func initJiggler() {
	ensureConfigLoaded()

	// A cold boot starts at the UNIX epoch (no battery-backed RTC) and time
	// sync later jumps the clock forward by decades. gocron reacts to a
	// next-run in the past by walking the cron expression forward one step at
	// a time: ~30M steps from 1970 for a per-minute schedule, over an hour of
	// pegged CPU with no jiggles, and it cannot be interrupted once started.
	//
	// Time sync tells us before it moves the clock, so the schedule is torn
	// down on the old clock and rebuilt on the new one, whenever the jump
	// happens. Until then a schedule on the epoch clock is harmless: it
	// jiggles on time, and the inactivity check is monotonic. Register the
	// hook before building, so a sync landing in between is not missed.
	if timeSync != nil {
		timeSync.SetClockHook(timesync.ClockHook{
			Before: jigglerBeforeClockSet,
			After:  jigglerAfterClockSet,
		})
	}
	startJigglerCronTab()
}

// jigglerBeforeClockSet runs on the old clock, just before time sync moves it.
// A schedule built on the old clock must not see a large jump: gocron would
// walk the cron expression from the old time to the new one inside its event
// loop. So the schedule is stopped here and rebuilt by jigglerAfterClockSet.
// Marking it pending keeps a config change arriving in between from building
// on the old clock.
func jigglerBeforeClockSet(jump time.Duration) {
	if jump.Abs() < jigglerClockJumpLimit {
		return
	}
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	logger.Info().Dur("jump", jump).Msg("clock about to jump, stopping jiggler schedule")
	jigglerSchedulePending = true
	shutdownJigglerSchedulerLocked()
}

// jigglerAfterClockSet runs on the new clock. It rebuilds the schedule that
// jigglerBeforeClockSet stopped, and leaves a running one alone otherwise.
func jigglerAfterClockSet(jump time.Duration) {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if !jigglerSchedulePending {
		return
	}
	logger.Info().Dur("jump", jump).Msg("clock set, building jiggler schedule")
	buildJigglerCronTabLocked()
}

func startJigglerCronTab() {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	buildJigglerCronTabLocked()
}

// buildJigglerCronTabLocked builds the schedule from the current config on the
// current clock, replacing any existing one. Caller must hold schedulerLock.
func buildJigglerCronTabLocked() {
	jigglerSchedulePending = false
	if err := replaceJigglerCronTabLocked(); err != nil {
		logger.Error().Err(err).Msg("error scheduling jiggler crontab")
	}
}

// rebuildJigglerCronTab rebuilds the schedule from the current config, unless
// a build is pending: doing it now would put the schedule on a clock that is
// about to change. The pending build reads the config when it runs, so the new
// settings still apply.
func rebuildJigglerCronTab() error {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if jigglerSchedulePending {
		logger.Info().Msg("clock is changing, jiggler config saved for the pending schedule")
		return nil
	}
	return replaceJigglerCronTabLocked()
}

// replaceJigglerCronTabLocked rebuilds the schedule. Caller must hold schedulerLock.
func replaceJigglerCronTabLocked() error {
	shutdownJigglerSchedulerLocked()
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
		gocron.NewTask(runJiggler),
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

// runJiggler is the scheduled task. gocron passes the job's context and cancels
// it when the schedule is replaced, on a config change or a clock jump, so the
// jitter wait must not outlive that: the replacement would otherwise block on
// it for gocron's stop timeout.
func runJiggler(ctx context.Context) {
	if config.JigglerEnabled {
		if config.JigglerConfig.JitterPercentage != 0 {
			jitter := calculateJitterDuration(jobDelta)
			if !waitUnlessCancelled(ctx, jitter) {
				return
			}
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

// waitUnlessCancelled waits for d, reporting false if ctx is cancelled first.
func waitUnlessCancelled(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
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
