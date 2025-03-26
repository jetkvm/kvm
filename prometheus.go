package kvm

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

var promHandler http.Handler

func initPrometheus() {
	// A Prometheus metrics endpoint.
	version.Version = builtAppVersion
	prometheus.MustRegister(versioncollector.NewCollector("jetkvm"))

	promHandler = promhttp.Handler()
}

func prometheusCheckAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.MetricsEnabled {
			c.JSON(http.StatusNotFound, gin.H{"error": "Metrics endpoint is disabled"})
			return
		}

		c.Next()
	}
}
