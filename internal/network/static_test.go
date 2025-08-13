package network

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyIPv4Static(t *testing.T) {
	assert.NoError(t, os.Setenv("JETKVM_DEBUG", "1"))

	// conf := &NetworkConfig{
	// 	IPv4Mode: null.StringFrom("dhcp"),
	// 	// IPv4Static: &IPv4StaticConfig{
	// 	// 	Address: null.StringFrom("203.0.113.100"),
	// 	// 	Netmask: null.StringFrom("255.255.255.0"),
	// 	// 	Gateway: null.StringFrom("203.0.113.1"),
	// 	// },
	// 	IPv6Mode: null.StringFrom("disabled"),
	// }
	// ifc, err := NewNetworkInterfaceConfig("eth0", conf, nil)
	// assert.NoError(t, err)

	// assert.NoError(t, ifc.Apply())
}
