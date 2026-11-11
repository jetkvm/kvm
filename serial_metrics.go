package kvm

import (
	"errors"
	"sync"

	"github.com/jetkvm/kvm/internal/serialnet"
	"github.com/prometheus/client_golang/prometheus"
	"go.bug.st/serial"
)

// serialPortTracker feeds the extension port metrics (see
// serialnet.NewCollector); every open, close and mode change goes through it.
var serialPortTracker = &serialnet.PortState{}

// setSerialPortMode applies m to the extension port and records it for the
// metrics. The port is nil when /dev/ttyS3 failed to open.
func setSerialPortMode(m *serial.Mode) error {
	if port == nil {
		return errors.New("serial port is not open")
	}
	if err := port.SetMode(m); err != nil {
		return err
	}
	serialPortTracker.SetMode(*m)
	return nil
}

var serialMetricsRegistered sync.Once

func registerSerialMetrics() {
	serialMetricsRegistered.Do(func() {
		prometheus.MustRegister(serialnet.NewCollector(
			serialPortPath,
			serialPortTracker,
			func() string { return config.ActiveExtension },
			serialNetwork.Status,
		))
	})
}
