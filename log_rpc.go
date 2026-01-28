package kvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jetkvm/kvm/internal/audio"
	"github.com/jetkvm/kvm/internal/crypto/tls"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/native"
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

	// Update all C subsystem log levels
	syncCLogLevels(overrides)

	// Save to config for persistence
	config.LogLevelOverrides = overrides
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	logger.Info().Str("overrides", overrides).Msg("log levels updated")
	return nil
}

// isValidLogLevel checks if a level name is valid (exists in nativeLevelMap).
func isValidLogLevel(level string) bool {
	_, ok := nativeLevelMap[strings.ToUpper(level)]
	return ok
}

// validateLogOverrides validates the log level override string format.
func validateLogOverrides(overrides string) error {
	if overrides == "" {
		return nil
	}

	for _, part := range strings.Split(overrides, ",") {
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
			level := strings.TrimSpace(kv[1])
			if subsystem == "" {
				return fmt.Errorf("empty subsystem name in: %s", part)
			}
			if !isValidLogLevel(level) {
				return fmt.Errorf("invalid log level '%s' in: %s", level, part)
			}
		} else {
			// Global level
			if !isValidLogLevel(part) {
				return fmt.Errorf("invalid log level: %s", part)
			}
		}
	}

	return nil
}

// C log level maps - defined once to avoid allocation on every call.
// Audio C levels: DISABLE=-1, PANIC=0, FATAL=1, ERROR=2, WARN=3, INFO=4, DEBUG=5, TRACE=6
// Native/zerolog levels: TRACE=-1, DEBUG=0, INFO=1, WARN=2, ERROR=3, FATAL=4, PANIC=5, DISABLE=6
var (
	audioLevelMap = map[string]int{
		"DISABLE": -1, "PANIC": 0, "FATAL": 1, "ERROR": 2,
		"WARN": 3, "INFO": 4, "DEBUG": 5, "TRACE": 6,
	}
	nativeLevelMap = map[string]int{
		"TRACE": -1, "DEBUG": 0, "INFO": 1, "WARN": 2,
		"ERROR": 3, "FATAL": 4, "PANIC": 5, "DISABLE": 6,
	}
)

// parseGlobalLogLevel extracts the global log level from an overrides string
// using the provided level map and default value.
func parseGlobalLogLevel(overrides string, defaultLevel int, levelMap map[string]int) int {
	level := defaultLevel
	for _, part := range strings.Split(overrides, ",") {
		part = strings.TrimSpace(part)
		if part != "" && !strings.Contains(part, ":") {
			if l, ok := levelMap[strings.ToUpper(part)]; ok {
				level = l
			}
		}
	}
	return level
}

// syncCLogLevels updates all C subsystem log levels from an overrides string.
func syncCLogLevels(overrides string) {
	syncCLogLevelsWithDefault(overrides, "WARN")
}

// syncCLogLevelsWithDefault updates all C subsystem log levels with a custom default.
// Called by config loading which may have a different default level.
func syncCLogLevelsWithDefault(overrides string, defaultLevel string) {
	// Get base levels from default, then override from string
	audioBase := 3 // WARN default for audio
	nativeBase := 2 // WARN default for native
	if defaultLevel != "" {
		if l, ok := audioLevelMap[strings.ToUpper(defaultLevel)]; ok {
			audioBase = l
		}
		if l, ok := nativeLevelMap[strings.ToUpper(defaultLevel)]; ok {
			nativeBase = l
		}
	}

	cLevel := parseGlobalLogLevel(overrides, audioBase, audioLevelMap)
	nativeLevel := parseGlobalLogLevel(overrides, nativeBase, nativeLevelMap)
	audio.SetCLogLevel(cLevel)
	native.SetCLogLevel(nativeLevel)
	tls.SetCLogLevel(nativeLevel)
}
