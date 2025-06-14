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

	if s.config.LLDPMode.String == "disabled" {
		return false
	}

	return true
}

func (s *NetworkInterfaceState) startLLDP() {
	if !s.shouldStartLLDP() || s.lldp == nil {
		return
	}

	s.l.Trace().Msg("starting LLDP")
	s.lldp.Start()
}

func (s *NetworkInterfaceState) stopLLDP() {
	if s.lldp == nil {
		return
	}
	s.l.Trace().Msg("stopping LLDP")
	s.lldp.Stop()
}

func (s *NetworkInterfaceState) GetLLDPNeighbors() ([]lldp.Neighbor, error) {
	if s.lldp == nil {
		return nil, errors.New("lldp not initialized")
	}
	return s.lldp.GetNeighbors(), nil
}
