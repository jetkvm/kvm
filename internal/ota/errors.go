package ota

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog"
)

var (
	// ErrVersionNotFound is returned when the specified version is not found
	ErrVersionNotFound = errors.New("specified version not found")
)

func (s *State) componentUpdateError(prefix string, err error, logger *zerolog.Logger) error {
	logger.Error().Err(err).Msg(prefix)
	s.error = fmt.Sprintf("%s: %v", prefix, err)
	s.updating = false
	s.triggerStateUpdate()
	return err
}
