package nmlite

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"golang.org/x/net/idna"
)

const (
	hostnamePath = "/etc/hostname"
	hostsPath    = "/etc/hosts"
)

var (
	hostnameLock sync.Mutex
)

// HostnameManager manages system hostname and /etc/hosts
type HostnameManager struct {
	logger *zerolog.Logger
}

// NewHostnameManager creates a new hostname manager
func NewHostnameManager(logger *zerolog.Logger) *HostnameManager {
	if logger == nil {
		// Create a no-op logger if none provided
		logger = &zerolog.Logger{}
	}

	return &HostnameManager{
		logger: logger,
	}
}

// SetHostname sets the system hostname and updates /etc/hosts
func (hm *HostnameManager) SetHostname(hostname, fqdn string) error {
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
func (hm *HostnameManager) GetCurrentHostname() (string, error) {
	return os.Hostname()
}

// GetCurrentFQDN returns the current FQDN
func (hm *HostnameManager) GetCurrentFQDN() (string, error) {
	hostname, err := hm.GetCurrentHostname()
	if err != nil {
		return "", err
	}

	// Try to get the FQDN from /etc/hosts
	return hm.getFQDNFromHosts(hostname)
}

// updateEtcHostname updates the /etc/hostname file
func (hm *HostnameManager) updateEtcHostname(hostname string) error {
	if err := os.WriteFile(hostnamePath, []byte(hostname), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hostnamePath, err)
	}

	hm.logger.Debug().Str("file", hostnamePath).Str("hostname", hostname).Msg("updated /etc/hostname")
	return nil
}

// updateEtcHosts updates the /etc/hosts file
func (hm *HostnameManager) updateEtcHosts(hostname, fqdn string) error {
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
func (hm *HostnameManager) setSystemHostname(hostname string) error {
	cmd := exec.Command("hostname", "-F", hostnamePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run hostname command: %w", err)
	}

	hm.logger.Debug().Str("hostname", hostname).Msg("set system hostname")
	return nil
}

// getFQDNFromHosts tries to get the FQDN from /etc/hosts
func (hm *HostnameManager) getFQDNFromHosts(hostname string) (string, error) {
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
	if hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}

	validHostname := ToValidHostname(hostname)
	if validHostname != hostname {
		return fmt.Errorf("hostname contains invalid characters: %s", hostname)
	}

	if len(hostname) > 253 {
		return fmt.Errorf("hostname too long: %d characters (max 253)", len(hostname))
	}

	// Check for valid characters (alphanumeric, hyphens, dots)
	for _, char := range hostname {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '.') {
			return fmt.Errorf("hostname contains invalid character: %c", char)
		}
	}

	// Check that it doesn't start or end with hyphen
	if strings.HasPrefix(hostname, "-") || strings.HasSuffix(hostname, "-") {
		return fmt.Errorf("hostname cannot start or end with hyphen")
	}

	// Check that it doesn't start or end with dot
	if strings.HasPrefix(hostname, ".") || strings.HasSuffix(hostname, ".") {
		return fmt.Errorf("hostname cannot start or end with dot")
	}

	return nil
}
