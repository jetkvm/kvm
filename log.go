package kvm

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/pion/logging"
	"github.com/rs/zerolog"
)

const defaultLogLevel = zerolog.ErrorLevel

// we use logging framework from pion
// ref: https://github.com/pion/webrtc/wiki/Debugging-WebRTC
var rootLogger = logging.NewDefaultLoggerFactory().NewLogger("jetkvm").GetLogger()

var (
	scopeLevels     map[string]zerolog.Level
	scopeLevelMutex = sync.Mutex{}
)

var (
	logger          = getLogger("jetkvm")
	cloudLogger     = getLogger("cloud")
	websocketLogger = getLogger("websocket")
	nativeLogger    = getLogger("native")
	ntpLogger       = getLogger("ntp")
	displayLogger   = getLogger("display")
	usbLogger       = getLogger("usb")
)

func updateLogLevel() {
	scopeLevelMutex.Lock()
	defer scopeLevelMutex.Unlock()

	defaultLevel := defaultLogLevel
	logLevels := map[string]zerolog.Level{
		"DISABLE": zerolog.Disabled,
		"NOLEVEL": zerolog.NoLevel,
		"PANIC":   zerolog.PanicLevel,
		"FATAL":   zerolog.FatalLevel,
		"ERROR":   zerolog.ErrorLevel,
		"WARN":    zerolog.WarnLevel,
		"INFO":    zerolog.InfoLevel,
		"DEBUG":   zerolog.DebugLevel,
		"TRACE":   zerolog.TraceLevel,
	}

	scopeLevels = make(map[string]zerolog.Level)

	for name, level := range logLevels {
		env := os.Getenv(fmt.Sprintf("JETKVM_LOG_%s", name))

		if env == "" {
			env = os.Getenv(fmt.Sprintf("PION_LOG_%s", name))
		}

		if env == "" {
			env = os.Getenv(fmt.Sprintf("PIONS_LOG_%s", name))
		}

		if env == "" {
			continue
		}

		if strings.ToLower(env) == "all" {
			if defaultLevel < level {
				defaultLevel = level
			}

			continue
		}

		scopes := strings.Split(strings.ToLower(env), ",")
		for _, scope := range scopes {
			scopeLevels[scope] = level
		}
	}
}

func getLogger(scope string) zerolog.Logger {
	if scopeLevels == nil {
		updateLogLevel()
	}

	l := rootLogger.With().Str("component", scope).Logger()

	// if the scope is not in the map, use the default level from the root logger
	if level, ok := scopeLevels[scope]; ok {
		return l.Level(level)
	}

	return l
}
