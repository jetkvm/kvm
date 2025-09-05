package audio

import (
	"os"
	"strconv"

	"github.com/jetkvm/kvm/internal/logging"
)

// getEnvInt reads an integer value from environment variable with fallback to default
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// parseOpusConfig reads OPUS configuration from environment variables
// with fallback to default config values
func parseOpusConfig() (bitrate, complexity, vbr, signalType, bandwidth, dtx int) {
	// Read configuration from environment variables with config defaults
	bitrate = getEnvInt("JETKVM_OPUS_BITRATE", GetConfig().CGOOpusBitrate)
	complexity = getEnvInt("JETKVM_OPUS_COMPLEXITY", GetConfig().CGOOpusComplexity)
	vbr = getEnvInt("JETKVM_OPUS_VBR", GetConfig().CGOOpusVBR)
	signalType = getEnvInt("JETKVM_OPUS_SIGNAL_TYPE", GetConfig().CGOOpusSignalType)
	bandwidth = getEnvInt("JETKVM_OPUS_BANDWIDTH", GetConfig().CGOOpusBandwidth)
	dtx = getEnvInt("JETKVM_OPUS_DTX", GetConfig().CGOOpusDTX)

	return bitrate, complexity, vbr, signalType, bandwidth, dtx
}

// applyOpusConfig applies OPUS configuration to the global config
// with optional logging for the specified component
func applyOpusConfig(bitrate, complexity, vbr, signalType, bandwidth, dtx int, component string, enableLogging bool) {
	config := GetConfig()
	config.CGOOpusBitrate = bitrate
	config.CGOOpusComplexity = complexity
	config.CGOOpusVBR = vbr
	config.CGOOpusSignalType = signalType
	config.CGOOpusBandwidth = bandwidth
	config.CGOOpusDTX = dtx

	if enableLogging {
		logger := logging.GetDefaultLogger().With().Str("component", component).Logger()
		logger.Info().
			Int("bitrate", bitrate).
			Int("complexity", complexity).
			Int("vbr", vbr).
			Int("signal_type", signalType).
			Int("bandwidth", bandwidth).
			Int("dtx", dtx).
			Msg("applied OPUS configuration")
	}
}
