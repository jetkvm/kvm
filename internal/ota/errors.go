package ota

import (
	"errors"
	"fmt"

	"github.com/jetkvm/kvm/internal/logging"
)

var (
	// ErrVersionNotFound is returned when the specified version is not found
	ErrVersionNotFound = errors.New("specified version not found")
)

func (s *State) componentUpdateError(prefix string, err error, logger *logging.Context) error {
	logger.Err(err).Error().Msg(prefix)
	s.error = fmt.Sprintf("%s: %v", prefix, err)
	s.updating = false
	s.triggerStateUpdate()
	return err
}
