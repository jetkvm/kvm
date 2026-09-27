package serialnet

import (
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.bug.st/serial"
)

// Metrics describing the extension port UART and its network access. They
// are computed at scrape time, so a line settings change (from the UI, an
// extension, or an RFC 2217 client) never leaves a stale info series behind.

var (
	descSerialPortOpen = prometheus.NewDesc(
		"jetkvm_serial_port_open",
		"Whether the extension port UART is open (1) or not (0)",
		[]string{"device"}, nil,
	)
	descSerialPortInfo = prometheus.NewDesc(
		"jetkvm_serial_port_info",
		"Current line settings of the extension port UART; always 1 while the port is open",
		[]string{"device", "baud_rate", "data_bits", "parity", "stop_bits", "extension"}, nil,
	)
	descSerialPortBaudRate = prometheus.NewDesc(
		"jetkvm_serial_port_baud_rate",
		"Current baud rate of the extension port UART",
		[]string{"device"}, nil,
	)
	descSerialNetworkInfo = prometheus.NewDesc(
		"jetkvm_serial_network_info",
		"Configured network access to the extension port UART; always 1",
		[]string{"mode", "protocol", "listen_address"}, nil,
	)
	descSerialNetworkUp = prometheus.NewDesc(
		"jetkvm_serial_network_up",
		"Whether network access to the extension port UART is serving (1) or not (0)",
		[]string{"mode"}, nil,
	)
	descSerialNetworkMaxClients = prometheus.NewDesc(
		"jetkvm_serial_network_max_clients",
		"Maximum concurrent network clients allowed on the extension port UART",
		nil, nil,
	)
)

// PortState records what was last applied to the port. go.bug.st/serial
// can't report a port's current mode, and the collector runs on the scrape
// goroutine, so the owner of the port updates this whenever it opens,
// closes or reconfigures it.
type PortState struct {
	mu   sync.Mutex
	open bool
	mode serial.Mode
}

func (s *PortState) Opened(m serial.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open, s.mode = true, m
}

func (s *PortState) Closed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = false
}

func (s *PortState) SetMode(m serial.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

func (s *PortState) snapshot() (bool, serial.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open, s.mode
}

func parityName(p serial.Parity) string {
	switch p {
	case serial.NoParity:
		return "none"
	case serial.OddParity:
		return "odd"
	case serial.EvenParity:
		return "even"
	case serial.MarkParity:
		return "mark"
	case serial.SpaceParity:
		return "space"
	}
	return "unknown"
}

func stopBitsName(s serial.StopBits) string {
	switch s {
	case serial.OneStopBit:
		return "1"
	case serial.OnePointFiveStopBits:
		return "1.5"
	case serial.TwoStopBits:
		return "2"
	}
	return "unknown"
}

// NewCollector reports the port behind state as device, the loaded
// extension, and the network access status.
func NewCollector(device string, state *PortState, extension func() string, network func() Status) prometheus.Collector {
	return &collector{device: device, state: state, extension: extension, network: network}
}

type collector struct {
	device    string
	state     *PortState
	extension func() string
	network   func() Status
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descSerialPortOpen
	ch <- descSerialPortInfo
	ch <- descSerialPortBaudRate
	ch <- descSerialNetworkInfo
	ch <- descSerialNetworkUp
	ch <- descSerialNetworkMaxClients
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	open, mode := c.state.snapshot()
	ch <- prometheus.MustNewConstMetric(descSerialPortOpen, prometheus.GaugeValue, boolToFloat(open), c.device)
	if open {
		extension := c.extension()
		if extension == "" {
			extension = "none"
		}
		ch <- prometheus.MustNewConstMetric(descSerialPortInfo, prometheus.GaugeValue, 1,
			c.device,
			strconv.Itoa(mode.BaudRate),
			strconv.Itoa(mode.DataBits),
			parityName(mode.Parity),
			stopBitsName(mode.StopBits),
			extension,
		)
		ch <- prometheus.MustNewConstMetric(descSerialPortBaudRate, prometheus.GaugeValue, float64(mode.BaudRate), c.device)
	}

	st := c.network()
	ch <- prometheus.MustNewConstMetric(descSerialNetworkInfo, prometheus.GaugeValue, 1, st.Mode, st.Protocol, st.ListenAddress)
	ch <- prometheus.MustNewConstMetric(descSerialNetworkUp, prometheus.GaugeValue, boolToFloat(st.Running), st.Mode)
	ch <- prometheus.MustNewConstMetric(descSerialNetworkMaxClients, prometheus.GaugeValue, float64(st.MaxClients))
}
