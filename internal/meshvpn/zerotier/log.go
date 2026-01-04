//go:build linux

package zerotier

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var logger zerolog.Logger

func init() {
	logger = log.With().Str("component", "zerotier").Logger()
}
