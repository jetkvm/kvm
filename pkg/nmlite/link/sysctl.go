package link

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

func (nm *NetlinkManager) setSysctlValues(ifaceName string, values map[string]int) error {
	for name, value := range values {
		name = fmt.Sprintf(name, ifaceName)
		name = strings.ReplaceAll(name, ".", "/")

		if err := os.WriteFile(path.Join(sysctlBase, name), []byte(strconv.Itoa(value)), sysctlFileMode); err != nil {
			return fmt.Errorf("failed to set sysctl %s=%d: %w", name, value, err)
		}
	}
	return nil
}

// EnableIPv6 enables IPv6 on the interface
func (nm *NetlinkManager) EnableIPv6(ifaceName string) error {
	return nm.setSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    2,
	})
}

// DisableIPv6 disables IPv6 on the interface
func (nm *NetlinkManager) DisableIPv6(ifaceName string) error {
	return nm.setSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 1,
	})
}

// EnableIPv6SLAAC enables IPv6 SLAAC on the interface
func (nm *NetlinkManager) EnableIPv6SLAAC(ifaceName string) error {
	return nm.setSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    2,
	})
}

// EnableIPv6LinkLocal enables IPv6 link-local only on the interface
func (nm *NetlinkManager) EnableIPv6LinkLocal(ifaceName string) error {
	return nm.setSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    0,
	})
}
