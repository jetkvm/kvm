package kvm

import (
	"github.com/go-co-op/gocron/v2"
	"math/rand"
	"time"
)

type JigglerConfig struct {
	InactivityLimitSeconds float64 `json:"inactivity_limit_seconds"`
	JitterPercentage       float64 `json:"jitter_percentage"`
	ScheduleCronTab        string  `json:"schedule_cron_tab"`
}

var lastUserInput = time.Now()
var jigglerEnabled = false
var jobDelta time.Duration = 0
var scheduler gocron.Scheduler = nil

func rpcSetJigglerState(enabled bool) {
	jigglerEnabled = enabled
}
func rpcGetJigglerState() bool {
	return jigglerEnabled
}

func rpcGetJigglerConfig() (JigglerConfig, error) {
	return *config.JigglerConfig, nil
}

func rpcSetJigglerConfig(jigglerConfig JigglerConfig) {
	config.JigglerConfig = &jigglerConfig
	err := removeExistingCrobJobs(scheduler)
	if err != nil {
		logger.Errorf("Error removing cron jobs from scheduler %v", err)
		return
	}
	err = runJigglerCronTab()
	if err != nil {
		logger.Errorf("Error scheduling jiggler crontab: %v", err)
		return
	}
}

func removeExistingCrobJobs(s gocron.Scheduler) error {
	for _, j := range s.Jobs() {
		err := s.RemoveJob(j.ID())
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	ensureConfigLoaded()
	err := runJigglerCronTab()
	if err != nil {
		logger.Errorf("Error scheduling jiggler crontab: %v", err)
		return
	}
}

func runJigglerCronTab() error {
	cronTab := config.JigglerConfig.ScheduleCronTab
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
	logger.Infof("Time between jiggler runs: %v", jobDelta)
	if err != nil {
		return err
	}
	return nil
}

func runJiggler() {
	if jigglerEnabled {
		if config.JigglerConfig.JitterPercentage != 0 {
			jitter := calculateJitterDuration(jobDelta)
			logger.Debugf("Jitter enabled, Sleeping for %v", jitter)
			time.Sleep(jitter)
		}
		inactivitySeconds := config.JigglerConfig.InactivityLimitSeconds
		if time.Since(lastUserInput) > time.Duration(inactivitySeconds)*time.Second {
			//TODO: change to rel mouse
			err := rpcAbsMouseReport(1, 1, 0)
			if err != nil {
				logger.Warnf("Failed to jiggle mouse: %v", err)
			}
			err = rpcAbsMouseReport(0, 0, 0)
			if err != nil {
				logger.Warnf("Failed to reset mouse position: %v", err)
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
	jitter := rand.Float64() * config.JigglerConfig.JitterPercentage * delta.Seconds()
	return time.Duration(jitter * float64(time.Second))
}
