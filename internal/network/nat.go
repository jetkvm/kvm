package network

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	procIpv4ForwardPath = "/proc/sys/net/ipv4/ip_forward"
)

func (s *NetworkInterfaceState) reconfigureNat(wantNat bool, sourceAddr string) error {
	scopedLogger := s.l.With().Str("iface", s.interfaceName).Logger()

	if wantNat && s.IsOnline() {
		scopedLogger.Info().Msg("enabling NAT")
		err := enableNat(sourceAddr, s.interfaceName, s.IPv4String())
		if err != nil {
			s.l.Error().Err(err).Msg("failed to enable NAT")
		}
	} else {
		scopedLogger.Info().Msg("disabling NAT")
		err := disableNat()
		if err != nil {
			s.l.Error().Err(err).Msg("failed to disable NAT")
		}
	}
	return nil
}

func enableNat(sourceAddr string, oIfName string, snatToAddr string) error {
	if err := os.WriteFile(procIpv4ForwardPath, []byte("1"), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", procIpv4ForwardPath, err)
	}

	if err := exec.Command("nft", "add table nat").Run(); err != nil {
		return fmt.Errorf("failed to add table nat: %w", err)
	}

	if err := exec.Command("nft", "flush table nat").Run(); err != nil {
		return fmt.Errorf("failed to flush table nat: %w", err)
	}

	if err := exec.Command("nft", "add chain nat postrouting { type nat hook postrouting priority 100 ; }").Run(); err != nil {
		return fmt.Errorf("failed to add chain nat: %w", err)
	}

	if err := exec.Command("nft", "add rule nat postrouting ip saddr", sourceAddr, "oif", oIfName, "snat to", snatToAddr).Run(); err != nil {
		return fmt.Errorf("failed to add postrouting rule: %w", err)
	}

	return nil
}

func disableNat() error {
	if err := os.WriteFile(procIpv4ForwardPath, []byte("0"), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", procIpv4ForwardPath, err)
	}

	if err := exec.Command("nft", "delete table nat").Run(); err != nil {
		return fmt.Errorf("failed to run nft: %w", err)
	}

	return nil
}