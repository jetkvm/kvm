package kvm

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
)

var promHandler http.Handler

func initPrometheus() {
	// A Prometheus metrics endpoint.
	version.Version = builtAppVersion
	prometheus.MustRegister(versioncollector.NewCollector("jetkvm"))
}
