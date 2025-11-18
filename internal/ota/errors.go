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

func (s *State) componentUpdateError(prefix string, err error, l *zerolog.Logger) error {
	if l == nil {
		l = s.l
	}
	l.Error().Err(err).Msg(prefix)
	s.error = fmt.Sprintf("%s: %v", prefix, err)
	s.updating = false
	s.triggerStateUpdate()
	return err
}
