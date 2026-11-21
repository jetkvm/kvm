package kvm

import (
	"sync"

	"github.com/jetkvm/kvm/internal/serialnet"
	"github.com/prometheus/client_golang/prometheus"
)

// serialPortTracker feeds the extension port metrics (see
// serialnet.NewCollector). extensionPort records every open, close and mode
// change in it, so a scrape never has to touch the port.
var serialPortTracker = &serialnet.PortState{}

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
