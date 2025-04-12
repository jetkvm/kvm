package websecure

import (
	"os"

	"github.com/rs/zerolog"
)

var defaultLogger = zerolog.New(os.Stdout).With().Str("component", "websecure").Logger()
