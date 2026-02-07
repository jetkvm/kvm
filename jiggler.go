package kvm

import (
	"fmt"
	"math/rand/v2"
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

func rpcSetJigglerState(enabled bool) error {
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.JigglerEnabled = enabled
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetJigglerState() bool {
	return loadCfg().JigglerEnabled
}

func rpcGetTimezones() []string {
	return tzdata.TimeZones
}

func rpcGetJigglerConfig() (JigglerConfig, error) {
	return *loadCfg().JigglerConfig, nil
}

func rpcSetJigglerConfig(jigglerConfig JigglerConfig) error {
	logger.Info().Msgf("jigglerConfig: %v, %v, %v, %v", jigglerConfig.InactivityLimitSeconds, jigglerConfig.JitterPercentage, jigglerConfig.ScheduleCronTab, jigglerConfig.Timezone)
	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.JigglerConfig = &jigglerConfig
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	err := removeExistingCronJobs(scheduler)
	if err != nil {
		return fmt.Errorf("error removing cron jobs from scheduler %v", err)
	}
	err = runJigglerCronTab()
	if err != nil {
		return fmt.Errorf("error scheduling jiggler crontab: %v", err)
	}
	return nil
}

func removeExistingCronJobs(s gocron.Scheduler) error {
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

func initJiggler() {
	err := runJigglerCronTab()
	if err != nil {
		logger.Error().Msgf("Error scheduling jiggler crontab: %v", err)
		return
	}
}

func runJigglerCronTab() error {
	jc := loadCfg().JigglerConfig
	cronTab := jc.ScheduleCronTab

	// Apply timezone if specified and valid
	if jc.Timezone != "" && jc.Timezone != "UTC" {
		// Validate timezone before applying
		if _, err := time.LoadLocation(jc.Timezone); err != nil {
			logger.Warn().Msgf("Invalid timezone '%s', falling back to UTC: %v", jc.Timezone, err)
			// Don't add TZ prefix, let it run in UTC
		} else {
			cronTab = fmt.Sprintf("TZ=%s %s", jc.Timezone, cronTab)
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
	cfg := loadCfg()
	if cfg.JigglerEnabled {
		if cfg.JigglerConfig.JitterPercentage != 0 {
			jitter := calculateJitterDuration(jobDelta)
			time.Sleep(jitter)
		}
		inactivitySeconds := cfg.JigglerConfig.InactivityLimitSeconds
		timeSinceLastInput := time.Since(gadget.GetLastUserInputTime())
		logger.Debug().Msgf("Time since last user input %v", timeSinceLastInput)
		if timeSinceLastInput > time.Duration(inactivitySeconds)*time.Second {
			logger.Debug().Msg("Jiggling mouse...")
			//TODO: change to rel mouse
			err := rpcAbsMouseReport(1, 1, 0)
			if err != nil {
				logger.Warn().Msgf("Failed to jiggle mouse: %v", err)
			}
			err = rpcAbsMouseReport(0, 0, 0)
			if err != nil {
				logger.Warn().Msgf("Failed to reset mouse position: %v", err)
			}
		}
	}
}

func calculateJobDelta(s gocron.Scheduler) (time.Duration, error) {
	jobs := s.Jobs()
	if len(jobs) == 0 {
		return 0, fmt.Errorf("no jobs in scheduler")
	}
	runs, err := jobs[0].NextRuns(2)
	if err != nil {
		return 0, err
	}
	if len(runs) < 2 {
		return 0, fmt.Errorf("could not determine next 2 runs")
	}
	return runs[1].Sub(runs[0]), nil
}

func calculateJitterDuration(delta time.Duration) time.Duration {
	jitter := rand.Float64() * float64(loadCfg().JigglerConfig.JitterPercentage) / 100 * delta.Seconds()
	return time.Duration(jitter * float64(time.Second))
}
