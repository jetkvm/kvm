package kvm

import (
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/powersched"

	"github.com/go-co-op/gocron/v2"
	"github.com/rs/zerolog"
)

// powerScheduler is deliberately a separate gocron instance from the jiggler's:
// rpcSetJigglerConfig removes *every* job on its scheduler, which would silently
// drop all power schedules if the two shared one.
var powerScheduler gocron.Scheduler

func initPowerScheduler() {
	ensureConfigLoaded()
	if err := rebuildPowerScheduler(); err != nil {
		powerSchedLogger.Error().Err(err).Msg("failed to initialize power scheduler")
	}
}

// rebuildPowerScheduler tears down the existing scheduler and re-registers a job
// for every enabled schedule. Disabled schedules stay in the config but get no
// job, which is how "temporarily disable" is implemented.
func rebuildPowerScheduler() error {
	if powerScheduler != nil {
		if err := powerScheduler.Shutdown(); err != nil {
			powerSchedLogger.Warn().Err(err).Msg("failed to shut down previous power scheduler")
		}
		powerScheduler = nil
	}

	s, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("failed to create power scheduler: %w", err)
	}
	powerScheduler = s

	scheduled := 0
	for i := range config.PowerSchedules {
		schedule := config.PowerSchedules[i]
		if !schedule.Enabled {
			continue
		}

		tab := schedule.CronTab()
		_, err := s.NewJob(
			gocron.CronJob(tab, true),
			gocron.NewTask(func() { runPowerSchedule(schedule) }),
		)
		if err != nil {
			// One bad schedule shouldn't prevent the others from running.
			powerSchedLogger.Error().Err(err).
				Str("id", schedule.ID).
				Str("name", schedule.Name).
				Str("crontab", tab).
				Msg("failed to schedule power action")
			continue
		}
		scheduled++
		powerSchedLogger.Info().
			Str("id", schedule.ID).
			Str("name", schedule.Name).
			Str("crontab", tab).
			Msg("power schedule registered")
	}

	s.Start()
	powerSchedLogger.Info().Int("scheduled", scheduled).Int("total", len(config.PowerSchedules)).Msg("power scheduler started")
	return nil
}

// runPowerSchedule executes a single schedule's action against the host.
func runPowerSchedule(s powersched.Schedule) {
	l := powerSchedLogger.With().
		Str("id", s.ID).
		Str("name", s.Name).
		Str("method", s.Method).
		Str("action", s.Action).
		Logger()

	// Cron jobs fire off the system clock; if NTP hasn't succeeded the device
	// clock may be far from the user's intended wall time.
	if timeSync != nil && !timeSync.IsSyncSuccess() {
		l.Warn().Msg("running power schedule while system time is not synced; firing time may be inaccurate")
	}

	var err error
	switch s.Method {
	case powersched.MethodWOL:
		err = runPowerScheduleWOL(s)
	case powersched.MethodATX:
		err = runPowerScheduleATX(s, &l)
	default:
		err = fmt.Errorf("unknown method: %s", s.Method)
	}

	if err != nil {
		l.Error().Err(err).Msg("power schedule failed")
		return
	}
	l.Info().Msg("power schedule executed")
}

func runPowerScheduleWOL(s powersched.Schedule) error {
	return rpcSendWOLMagicPacket(s.MacAddress, s.BroadcastIP)
}

func runPowerScheduleATX(s powersched.Schedule, l *zerolog.Logger) error {
	if config.ActiveExtension != "atx-power" {
		return fmt.Errorf("ATX power extension is not active")
	}

	// The power LED tells us the current state, so we can avoid pressing the
	// button when the host is already in the requested state - a stray press
	// would otherwise toggle a running machine off.
	powered := ledPWRState.Load()
	switch s.Action {
	case powersched.ActionOn:
		if powered {
			l.Info().Msg("host is already powered on, skipping")
			return nil
		}
		return pressATXPowerButton(200 * time.Millisecond)
	case powersched.ActionOff:
		if !powered {
			l.Info().Msg("host is already powered off, skipping")
			return nil
		}
		// Short press requests a graceful ACPI shutdown.
		return pressATXPowerButton(200 * time.Millisecond)
	case powersched.ActionOffForce:
		if !powered {
			l.Info().Msg("host is already powered off, skipping")
			return nil
		}
		// Long press cuts power regardless of OS state.
		return pressATXPowerButton(5 * time.Second)
	default:
		return fmt.Errorf("unknown action: %s", s.Action)
	}
}

func rpcGetPowerSchedules() ([]powersched.Schedule, error) {
	if config.PowerSchedules == nil {
		return []powersched.Schedule{}, nil
	}
	return config.PowerSchedules, nil
}

type SetPowerSchedulesParams struct {
	Schedules []powersched.Schedule `json:"schedules"`
}

func rpcSetPowerSchedules(params SetPowerSchedulesParams) error {
	schedules := params.Schedules
	if schedules == nil {
		schedules = []powersched.Schedule{}
	}

	if len(schedules) > powersched.MaxSchedules {
		return fmt.Errorf("too many schedules (max %d)", powersched.MaxSchedules)
	}

	ids := make(map[string]bool, len(schedules))
	for i := range schedules {
		if err := schedules[i].Validate(); err != nil {
			return fmt.Errorf("invalid schedule %q: %w", schedules[i].Name, err)
		}
		if schedules[i].ID == "" {
			return fmt.Errorf("schedule %q is missing an id", schedules[i].Name)
		}
		if ids[schedules[i].ID] {
			return fmt.Errorf("duplicate schedule id: %s", schedules[i].ID)
		}
		ids[schedules[i].ID] = true
	}

	config.PowerSchedules = schedules

	if err := rebuildPowerScheduler(); err != nil {
		return fmt.Errorf("failed to apply power schedules: %w", err)
	}

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}
