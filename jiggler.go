package kvm

import (
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

// jigglerSchedulePending is set while the schedule must not be built: while
// initJiggler waits for a trustworthy clock, and between time sync announcing a
// clock jump and completing it. A config change arriving in that window must
// not build the schedule itself, or it would put it back on the old clock.
var jigglerSchedulePending bool

// jigglerClockChanging is set between time sync announcing a clock jump and
// completing it. The offline fallback must not build in that window either:
// the After hook will, on the new clock.
var jigglerClockChanging bool

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

const (
	jigglerClockPollInterval = 5 * time.Second
	jigglerClockWaitLimit    = 10 * time.Minute
	// jigglerClockJumpLimit is the smallest clock change worth rebuilding the
	// schedule for. The hourly NTP corrections are milliseconds and gocron
	// absorbs them; anything larger costs it a catch-up step per schedule
	// interval skipped.
	jigglerClockJumpLimit = time.Minute
)

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
	// happens. Register the hook before looking at the clock, so a sync landing
	// in between is not missed.
	if timeSync != nil {
		timeSync.SetClockHook(timesync.ClockHook{
			Before: jigglerBeforeClockSet,
			After:  jigglerAfterClockSet,
		})
	}

	if !isTimeSyncNeeded() {
		startJigglerCronTab()
		return
	}

	schedulerLock.Lock()
	jigglerSchedulePending = true
	schedulerLock.Unlock()

	logger.Info().Msg("system clock is not yet trustworthy, deferring jiggler schedule until time sync")
	go func() {
		if !waitForTrustworthyClock(timeSyncSucceeded, jigglerClockPollInterval, jigglerClockWaitLimit) {
			// An offline device with no jiggler at all is the worse failure.
			// When it does come online, the clock hook replaces this schedule
			// before the clock jumps.
			logger.Warn().Msgf("clock still unsynced after %v, scheduling jiggler anyway", jigglerClockWaitLimit)
		}
		// On a synced clock the hook has normally built the schedule already;
		// this is a no-op then.
		startJigglerCronTabIfPending()
	}()
}

// timeSyncSucceeded is the quiet form of the clock check: isTimeSyncNeeded logs
// a warning on every call, which a poll loop would turn into log spam.
func timeSyncSucceeded() bool {
	return timeSync != nil && timeSync.IsSyncSuccess()
}

// waitForTrustworthyClock polls until synced reports true, reporting whether it
// did so within the limit. The elapsed check is monotonic, so the jump being
// waited on cannot skew it.
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
	jigglerClockChanging = true
	jigglerSchedulePending = true
	shutdownJigglerSchedulerLocked()
}

// jigglerAfterClockSet runs on the new clock. It builds the schedule if one is
// pending, whether jigglerBeforeClockSet stopped it or initJiggler deferred it
// on an untrustworthy clock, and leaves a running schedule alone otherwise.
func jigglerAfterClockSet(jump time.Duration) {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	jigglerClockChanging = false
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

// startJigglerCronTabIfPending builds the schedule only if nothing has since
// the deferral began, and not in the middle of a clock change: the After hook
// builds then.
func startJigglerCronTabIfPending() {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if !jigglerSchedulePending || jigglerClockChanging {
		return
	}
	buildJigglerCronTabLocked()
}

// buildJigglerCronTabLocked ends any deferral and builds the schedule from the
// current config on the current clock, replacing any existing one. Caller must
// hold schedulerLock.
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
		logger.Info().Msg("clock not settled yet, jiggler config saved for the pending schedule")
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
