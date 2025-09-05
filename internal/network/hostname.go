package network

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"golang.org/x/net/idna"
)

const (
	hostnamePath = "/etc/hostname"
	hostsPath    = "/etc/hosts"
)

var (
	hostnameLock sync.Mutex = sync.Mutex{}
)

func updateEtcHosts(hostname string, fqdn string) error {
	// update /etc/hosts
	hostsFile, err := os.OpenFile(hostsPath, os.O_RDWR|os.O_SYNC, os.ModeExclusive)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", hostsPath, err)
	}
	defer hostsFile.Close()

	// read all lines
	if _, err := hostsFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek %s: %w", hostsPath, err)
	}

	lines, err := io.ReadAll(hostsFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", hostsPath, err)
	}

	newLines := []string{}
	hostLine := fmt.Sprintf("127.0.1.1\t%s %s", hostname, fqdn)
	hostLineExists := false

	for line := range strings.SplitSeq(string(lines), "\n") {
		if strings.HasPrefix(line, "127.0.1.1") {
			hostLineExists = true
			line = hostLine
		}
		newLines = append(newLines, line)
	}

	if !hostLineExists {
		newLines = append(newLines, hostLine)
	}

	if err := hostsFile.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", hostsPath, err)
	}

	if _, err := hostsFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek %s: %w", hostsPath, err)
	}

	if _, err := hostsFile.Write([]byte(strings.Join(newLines, "\n"))); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostsPath, err)
	}

	return nil
}

func ToValidHostname(hostname string) string {
	ascii, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return ""
	}
	return ascii
}

func SetHostname(hostname string, fqdn string) error {
	hostnameLock.Lock()
	defer hostnameLock.Unlock()

	hostname = ToValidHostname(strings.TrimSpace(hostname))
	fqdn = ToValidHostname(strings.TrimSpace(fqdn))

	if hostname == "" {
		return fmt.Errorf("invalid hostname: %s", hostname)
	}

	if fqdn == "" {
		fqdn = hostname
	}

	// update /etc/hostname
	if err := os.WriteFile(hostnamePath, []byte(hostname), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostnamePath, err)
	}

	// update /etc/hosts
	if err := updateEtcHosts(hostname, fqdn); err != nil {
		return fmt.Errorf("failed to update /etc/hosts: %w", err)
	}

	// run hostname
	if err := exec.Command("hostname", "-F", hostnamePath).Run(); err != nil {
		return fmt.Errorf("failed to run hostname: %w", err)
	}

	return nil
}

func (s *NetworkInterfaceState) setHostnameIfNotSame() error {
	hostname := s.GetHostname()
	currentHostname, _ := os.Hostname()

	fqdn := fmt.Sprintf("%s.%s", hostname, s.GetDomain())

	if currentHostname == hostname && s.currentFqdn == fqdn && s.currentHostname == hostname {
		return nil
	}

	scopedLogger := s.l.With().Str("hostname", hostname).Str("fqdn", fqdn).Logger()

	err := SetHostname(hostname, fqdn)
	if err != nil {
		scopedLogger.Error().Err(err).Msg("failed to set hostname")
		return err
	}

	s.currentHostname = hostname
	s.currentFqdn = fqdn

	scopedLogger.Info().Msg("hostname set")

	return nil
}
