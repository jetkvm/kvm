package kvm

import (
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
)

func initPrometheus() {
	// A Prometheus metrics endpoint.
	version.Version = builtAppVersion
	prometheus.MustRegister(versioncollector.NewCollector("jetkvm"))
}
