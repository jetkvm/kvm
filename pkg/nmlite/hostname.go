package nmlite

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/net/idna"
)

const (
	hostnamePath = "/etc/hostname"
	hostsPath    = "/etc/hosts"
)

// SetHostname sets the system hostname and updates /etc/hosts
func (hm *ResolvConfManager) SetHostname(hostname, domain string) error {
	hostname = ToValidHostname(strings.TrimSpace(hostname))
	domain = ToValidHostname(strings.TrimSpace(domain))

	if hostname == "" {
		return fmt.Errorf("invalid hostname: %s", hostname)
	}

	hm.hostname = hostname
	hm.domain = domain

	return hm.reconcileHostname()
}

func (hm *ResolvConfManager) Domain() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.getDomain()
}

func (hm *ResolvConfManager) Hostname() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.getHostname()
}

func (hm *ResolvConfManager) FQDN() string {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.getFQDN()
}

func (hm *ResolvConfManager) getFQDN() string {
	hostname := hm.getHostname()
	domain := hm.getDomain()

	if domain == "" {
		return hostname
	}

	return fmt.Sprintf("%s.%s", hostname, domain)
}

func (hm *ResolvConfManager) getHostname() string {
	if hm.hostname != "" {
		return hm.hostname
	}
	return "jetkvm"
}

func (hm *ResolvConfManager) getDomain() string {
	if hm.domain != "" && hm.domain != "dhcp" {
		return hm.domain
	}

	for _, iface := range hm.conf.ConfigIPv4 {
		if iface.Domain != "" {
			return iface.Domain
		}
	}

	for _, iface := range hm.conf.ConfigIPv6 {
		if iface.Domain != "" {
			return iface.Domain
		}
	}

	return "local"
}

func (hm *ResolvConfManager) reconcileHostname() error {
	hm.mu.Lock()
	domain := hm.getDomain()
	hostname := hm.hostname
	if hostname == "" {
		hostname = "jetkvm"
	}
	hm.mu.Unlock()

	fqdn := hostname
	if fqdn != "" {
		fqdn = fmt.Sprintf("%s.%s", hostname, domain)
	}

	hm.logger.Info().
		Str("hostname", hostname).
		Str("fqdn", fqdn).
		Msg("setting hostname")

	// Update /etc/hostname
	if err := hm.updateEtcHostname(hostname); err != nil {
		return fmt.Errorf("failed to update /etc/hostname: %w", err)
	}

	// Update /etc/hosts
	if err := hm.updateEtcHosts(hostname, fqdn); err != nil {
		return fmt.Errorf("failed to update /etc/hosts: %w", err)
	}

	// Set the hostname using hostname command
	if err := hm.setSystemHostname(hostname); err != nil {
		return fmt.Errorf("failed to set system hostname: %w", err)
	}

	hm.logger.Info().
		Str("hostname", hostname).
		Str("fqdn", fqdn).
		Msg("hostname set successfully")

	return nil
}

// GetCurrentHostname returns the current system hostname
func (hm *ResolvConfManager) GetCurrentHostname() (string, error) {
	return os.Hostname()
}

// GetCurrentFQDN returns the current FQDN
func (hm *ResolvConfManager) GetCurrentFQDN() (string, error) {
	hostname, err := hm.GetCurrentHostname()
	if err != nil {
		return "", err
	}

	// Try to get the FQDN from /etc/hosts
	return hm.getFQDNFromHosts(hostname)
}

// updateEtcHostname updates the /etc/hostname file
func (hm *ResolvConfManager) updateEtcHostname(hostname string) error {
	if err := os.WriteFile(hostnamePath, []byte(hostname), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostnamePath, err)
	}

	hm.logger.Debug().Str("file", hostnamePath).Str("hostname", hostname).Msg("updated /etc/hostname")
	return nil
}

// updateEtcHosts updates the /etc/hosts file
func (hm *ResolvConfManager) updateEtcHosts(hostname, fqdn string) error {
	// Open /etc/hosts for reading and writing
	hostsFile, err := os.OpenFile(hostsPath, os.O_RDWR|os.O_SYNC, os.ModeExclusive)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", hostsPath, err)
	}
	defer hostsFile.Close()

	// Read all lines
	if _, err := hostsFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek %s: %w", hostsPath, err)
	}

	lines, err := io.ReadAll(hostsFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", hostsPath, err)
	}

	// Process lines
	newLines := []string{}
	hostLine := fmt.Sprintf("127.0.1.1\t%s %s", hostname, fqdn)
	hostLineExists := false

	for _, line := range strings.Split(string(lines), "\n") {
		if strings.HasPrefix(line, "127.0.1.1") {
			hostLineExists = true
			line = hostLine
		}
		newLines = append(newLines, line)
	}

	// Add host line if it doesn't exist
	if !hostLineExists {
		newLines = append(newLines, hostLine)
	}

	// Write back to file
	if err := hostsFile.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", hostsPath, err)
	}

	if _, err := hostsFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek %s: %w", hostsPath, err)
	}

	if _, err := hostsFile.Write([]byte(strings.Join(newLines, "\n"))); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostsPath, err)
	}

	hm.logger.Debug().
		Str("file", hostsPath).
		Str("hostname", hostname).
		Str("fqdn", fqdn).
		Msg("updated /etc/hosts")

	return nil
}

// setSystemHostname sets the system hostname using the hostname command
func (hm *ResolvConfManager) setSystemHostname(hostname string) error {
	cmd := exec.Command("hostname", "-F", hostnamePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run hostname command: %w", err)
	}

	hm.logger.Debug().Str("hostname", hostname).Msg("set system hostname")
	return nil
}

// getFQDNFromHosts tries to get the FQDN from /etc/hosts
func (hm *ResolvConfManager) getFQDNFromHosts(hostname string) (string, error) {
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return hostname, nil // Return hostname as fallback
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "127.0.1.1") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// The second part should be the FQDN
				return parts[1], nil
			}
		}
	}

	return hostname, nil // Return hostname as fallback
}

// ToValidHostname converts a hostname to a valid format
func ToValidHostname(hostname string) string {
	ascii, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return ""
	}
	return ascii
}

// ValidateHostname validates a hostname
func ValidateHostname(hostname string) error {
	_, err := idna.Lookup.ToASCII(hostname)
	return err
}
