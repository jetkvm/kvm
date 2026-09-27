package kvm

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.bug.st/serial"
)

func TestSerialCollector(t *testing.T) {
	state := &serialPortState{}
	network := SerialNetworkStatus{Mode: SerialNetworkModeDisabled, MaxClients: 1}
	extension := ""
	c := &serialCollector{
		device:    "/dev/ttyS3",
		state:     state,
		extension: func() string { return extension },
		network:   func() SerialNetworkStatus { return network },
	}

	// Closed port: no line settings are reported.
	want := `
# HELP jetkvm_serial_network_info Configured network access to the extension port UART; always 1
# TYPE jetkvm_serial_network_info gauge
jetkvm_serial_network_info{listen_address="",mode="disabled",protocol=""} 1
# HELP jetkvm_serial_network_max_clients Maximum concurrent network clients allowed on the extension port UART
# TYPE jetkvm_serial_network_max_clients gauge
jetkvm_serial_network_max_clients 1
# HELP jetkvm_serial_network_up Whether network access to the extension port UART is serving (1) or not (0)
# TYPE jetkvm_serial_network_up gauge
jetkvm_serial_network_up{mode="disabled"} 0
# HELP jetkvm_serial_port_open Whether the extension port UART is open (1) or not (0)
# TYPE jetkvm_serial_port_open gauge
jetkvm_serial_port_open{device="/dev/ttyS3"} 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}

	// Open, reconfigured (e.g. by an RFC 2217 client), serving ser2net.
	state.opened(serial.Mode{BaudRate: 115200, DataBits: 8})
	state.setMode(serial.Mode{BaudRate: 9600, DataBits: 7, Parity: serial.EvenParity, StopBits: serial.TwoStopBits})
	extension = "serial-console"
	network = SerialNetworkStatus{
		Mode:          SerialNetworkModeSer2Net,
		Protocol:      Ser2NetProtocolRFC2217,
		Running:       true,
		ListenAddress: "[::]:2217",
		MaxClients:    2,
	}
	want = `
# HELP jetkvm_serial_network_info Configured network access to the extension port UART; always 1
# TYPE jetkvm_serial_network_info gauge
jetkvm_serial_network_info{listen_address="[::]:2217",mode="ser2net",protocol="rfc2217"} 1
# HELP jetkvm_serial_network_max_clients Maximum concurrent network clients allowed on the extension port UART
# TYPE jetkvm_serial_network_max_clients gauge
jetkvm_serial_network_max_clients 2
# HELP jetkvm_serial_network_up Whether network access to the extension port UART is serving (1) or not (0)
# TYPE jetkvm_serial_network_up gauge
jetkvm_serial_network_up{mode="ser2net"} 1
# HELP jetkvm_serial_port_baud_rate Current baud rate of the extension port UART
# TYPE jetkvm_serial_port_baud_rate gauge
jetkvm_serial_port_baud_rate{device="/dev/ttyS3"} 9600
# HELP jetkvm_serial_port_info Current line settings of the extension port UART; always 1 while the port is open
# TYPE jetkvm_serial_port_info gauge
jetkvm_serial_port_info{baud_rate="9600",data_bits="7",device="/dev/ttyS3",extension="serial-console",parity="even",stop_bits="2"} 1
# HELP jetkvm_serial_port_open Whether the extension port UART is open (1) or not (0)
# TYPE jetkvm_serial_port_open gauge
jetkvm_serial_port_open{device="/dev/ttyS3"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}

	// Lint the names and help text the way promtool would.
	problems, err := testutil.CollectAndLint(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("lint: %s: %s", p.Metric, p.Text)
	}

	// Closing the port drops the settings series instead of leaving them stale.
	state.closed()
	if n := testutil.CollectAndCount(c, "jetkvm_serial_port_info", "jetkvm_serial_port_baud_rate"); n != 0 {
		t.Fatalf("got %d settings series for a closed port", n)
	}
}
