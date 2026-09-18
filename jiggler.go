package kvm

import (
	"fmt"
	"math/rand"
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
	err := removeExistingCrobJobs(scheduler)
	if err != nil {
		return fmt.Errorf("error removing cron jobs from scheduler %v", err)
	}
	err = runJigglerCronTab()
	if err != nil {
		return fmt.Errorf("error scheduling jiggler crontab: %v", err)
	}
	err = SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
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

func initJiggler() {
	ensureConfigLoaded()
	err := runJigglerCronTab()
	if err != nil {
		logger.Error().Msgf("Error scheduling jiggler crontab: %v", err)
		return
	}
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

// Absolute jiggle offsets are HID logical units, not pixels: the host scales
// them by round(hid/32767*displayWidth) (see ui/e2e/helpers/hid.ts). A raw
// offset of 1-3 quantizes to no visible movement on most displays, so use a
// small percentage of the full range instead. This guarantees a host-visible
// nudge regardless of display size while staying imperceptibly small.
const (
	absJiggleOffsetMin = 100
	absJiggleOffsetMax = 300
)

// Relative jiggle offsets are already host pixels (or close to it), so a
// small raw value is enough.
const (
	relJiggleOffsetMin = 1
	relJiggleOffsetMax = 3
)

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
			var err error
			_, _, absPositionKnown := gadget.GetAbsMousePosition()
			if gadget.HasAbsoluteMouse() && (absPositionKnown || !gadget.HasRelativeMouse()) {
				err = rpcJiggleAbsMouseReport(absJiggleOffsetMin, absJiggleOffsetMax)
			} else if gadget.HasRelativeMouse() {
				dx := randomSignedOffset(relJiggleOffsetMin, relJiggleOffsetMax)
				dy := randomSignedOffset(relJiggleOffsetMin, relJiggleOffsetMax)
				err = rpcRelMouseReport(int8(dx), int8(dy), 0)
			}
			if err != nil {
				logger.Warn().Msgf("Failed to jiggle mouse: %v", err)
			}
		}
	}
}

func randomSignedOffset(minMagnitude, maxMagnitude int) int {
	magnitude := rand.Intn(maxMagnitude-minMagnitude+1) + minMagnitude
	if rand.Intn(2) == 0 {
		magnitude = -magnitude
	}
	return magnitude
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
