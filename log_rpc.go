package kvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jetkvm/kvm/internal/logging"
)

// LogLevelState represents the current logging configuration state for the UI.
type LogLevelState struct {
	// GlobalLevel is the effective global log level
	GlobalLevel string `json:"globalLevel"`
	// SubsystemLevels maps subsystem names to their effective log levels
	SubsystemLevels map[string]string `json:"subsystemLevels"`
	// AvailableLevels is the list of valid log level names
	AvailableLevels []string `json:"availableLevels"`
	// Subsystems is the list of all registered subsystem names (sorted)
	Subsystems []string `json:"subsystems"`
	// Overrides is the current config override string (for display/editing)
	Overrides string `json:"overrides"`
}

// rpcGetLogLevelState returns the current logging state for the UI.
func rpcGetLogLevelState() (LogLevelState, error) {
	rootLogger := logging.GetRootLogger()

	subsystems := rootLogger.GetAvailableSubsystems()
	sort.Strings(subsystems)

	subsystemLevels := rootLogger.GetSubsystemLevels()

	// Determine global level from the first subsystem or default
	globalLevel := "WARN"
	if len(subsystemLevels) > 0 {
		// Find the most common level as the "effective global"
		levelCounts := make(map[string]int)
		for _, level := range subsystemLevels {
			levelCounts[level]++
		}
		maxCount := 0
		for level, count := range levelCounts {
			if count > maxCount {
				maxCount = count
				globalLevel = level
			}
		}
	}

	// Get the current config override string
	overrides := rootLogger.GetConfigOverrides()

	return LogLevelState{
		GlobalLevel:     globalLevel,
		SubsystemLevels: subsystemLevels,
		AvailableLevels: rootLogger.GetAvailableLevels(),
		Subsystems:      subsystems,
		Overrides:       overrides,
	}, nil
}

// SetLogLevelParams contains the parameters for setting log levels.
type SetLogLevelParams struct {
	Overrides string `json:"overrides"`
}

// rpcSetLogLevel sets the log level configuration.
// The overrides parameter can be:
// - A global level: "DEBUG"
// - Per-subsystem levels: "rdp:TRACE,vnc:DEBUG"
// - Combined: "INFO,rdp:TRACE,vnc:DEBUG"
func rpcSetLogLevel(params SetLogLevelParams) error {
	overrides := strings.TrimSpace(params.Overrides)

	// Validate the format before applying
	if err := validateLogOverrides(overrides); err != nil {
		return fmt.Errorf("invalid log level format: %w", err)
	}

	// Apply to logger immediately
	logging.GetRootLogger().SetSubsystemLevels(overrides)

	// Save to config for persistence
	config.LogLevelOverrides = overrides
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	logger.Info().Str("overrides", overrides).Msg("log levels updated")
	return nil
}

// validateLogOverrides validates the log level override string format.
func validateLogOverrides(overrides string) error {
	if overrides == "" {
		return nil
	}

	validLevels := map[string]bool{
		"DISABLE": true,
		"PANIC":   true,
		"FATAL":   true,
		"ERROR":   true,
		"WARN":    true,
		"INFO":    true,
		"DEBUG":   true,
		"TRACE":   true,
	}

	parts := strings.Split(overrides, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, ":") {
			// subsystem:LEVEL format
			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				return fmt.Errorf("invalid format: %s", part)
			}
			subsystem := strings.TrimSpace(kv[0])
			level := strings.TrimSpace(strings.ToUpper(kv[1]))
			if subsystem == "" {
				return fmt.Errorf("empty subsystem name in: %s", part)
			}
			if !validLevels[level] {
				return fmt.Errorf("invalid log level '%s' in: %s", level, part)
			}
		} else {
			// Global level
			level := strings.ToUpper(part)
			if !validLevels[level] {
				return fmt.Errorf("invalid log level: %s", part)
			}
		}
	}

	return nil
}
