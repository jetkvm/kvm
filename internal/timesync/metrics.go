package timesync

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricTimeSyncStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jetkvm_timesync_status",
			Help: "The status of the timesync, 1 if successful, 0 if not",
		},
	)
	metricTimeSyncCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_count",
			Help: "The number of times the timesync has been run",
		},
	)
	metricTimeSyncSuccessCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_success_count",
			Help: "The number of times the timesync has been successful",
		},
	)
	metricRTCUpdateCount = promauto.NewCounter( //nolint:unused
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_rtc_update_count",
			Help: "The number of times the RTC has been updated",
		},
	)
	metricNtpTotalSuccessCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_ntp_total_success_count",
			Help: "The total number of successful NTP requests",
		},
	)
	metricNtpTotalRequestCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_ntp_total_request_count",
			Help: "The total number of NTP requests sent",
		},
	)
	metricNtpSuccessCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_ntp_success_count",
			Help: "The number of successful NTP requests",
		},
		[]string{"url"},
	)
	metricNtpRequestCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_ntp_request_count",
			Help: "The number of NTP requests sent to the server",
		},
		[]string{"url"},
	)
	metricNtpServerLastRTT = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_timesync_ntp_server_last_rtt",
			Help: "The last RTT of the NTP server in milliseconds",
		},
		[]string{"url"},
	)
	metricNtpServerRttHistogram = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "jetkvm_timesync_ntp_server_rtt",
			Help: "The histogram of the RTT of the NTP server in milliseconds",
			Buckets: []float64{
				10, 25, 50, 100, 200, 300, 500, 1000,
			},
		},
		[]string{"url"},
	)
	metricNtpServerInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_timesync_ntp_server_info",
			Help: "The info of the NTP server",
		},
		[]string{"url", "reference", "stratum", "precision"},
	)

	metricHttpTotalSuccessCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_total_success_count",
			Help: "The total number of successful HTTP requests",
		},
	)
	metricHttpTotalRequestCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_total_request_count",
			Help: "The total number of HTTP requests sent",
		},
	)
	metricHttpTotalCancelCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_total_cancel_count",
			Help: "The total number of HTTP requests cancelled",
		},
	)
	metricHttpSuccessCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_success_count",
			Help: "The number of successful HTTP requests",
		},
		[]string{"url"},
	)
	metricHttpRequestCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_request_count",
			Help: "The number of HTTP requests sent to the server",
		},
		[]string{"url"},
	)
	metricHttpCancelCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_timesync_http_cancel_count",
			Help: "The number of HTTP requests cancelled",
		},
		[]string{"url"},
	)
	metricHttpServerLastRTT = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_timesync_http_server_last_rtt",
			Help: "The last RTT of the HTTP server in milliseconds",
		},
		[]string{"url"},
	)
	metricHttpServerRttHistogram = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "jetkvm_timesync_http_server_rtt",
			Help: "The histogram of the RTT of the HTTP server in milliseconds",
			Buckets: []float64{
				10, 25, 50, 100, 200, 300, 500, 1000,
			},
		},
		[]string{"url"},
	)
	metricHttpServerInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_timesync_http_server_info",
			Help: "The info of the HTTP server",
		},
		[]string{"url", "http_code"},
	)
)
