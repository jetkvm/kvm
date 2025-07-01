package network

import (
	"errors"

	"github.com/jetkvm/kvm/internal/lldp"
)

func (s *NetworkInterfaceState) shouldStartLLDP() bool {
	if s.lldp == nil {
		s.l.Trace().Msg("LLDP not initialized")
		return false
	}

	s.l.Trace().Msgf("LLDP mode: %s", s.config.LLDPMode.String)

	return s.config.LLDPMode.String != "disabled"
}

func (s *NetworkInterfaceState) startLLDP() {
	if !s.shouldStartLLDP() || s.lldp == nil {
		return
	}

	s.l.Trace().Msg("starting LLDP")
	if err := s.lldp.Start(); err != nil {
		s.l.Error().Err(err).Msg("unable to start LLDP")
	}
}

func (s *NetworkInterfaceState) stopLLDP() {
	if s.lldp == nil {
		return
	}
	s.l.Trace().Msg("stopping LLDP")
	if err := s.lldp.Stop(); err != nil {
		s.l.Error().Err(err).Msg("unable to stop LLDP")
	}
}

func (s *NetworkInterfaceState) GetLLDPNeighbors() ([]lldp.Neighbor, error) {
	if s.lldp == nil {
		return nil, errors.New("lldp not initialized")
	}
	return s.lldp.GetNeighbors(), nil
}
