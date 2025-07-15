package kvm

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	dcCurrentGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jetkvm_dc_current_amperes",
		Help: "Current DC power consumption in amperes",
	})

	dcPowerGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jetkvm_dc_power_watts",
		Help: "DC power consumption in watts",
	})

	dcVoltageGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jetkvm_dc_voltage_volts",
		Help: "DC voltage in volts",
	})

	dcStateGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "jetkvm_dc_power_state",
		Help: "DC power state (1 = on, 0 = off)",
	})

	dcMetricsRegistered sync.Once
)

// registerDCMetrics registers the DC power metrics with Prometheus (called once when DC control is mounted)
func registerDCMetrics() {
	dcMetricsRegistered.Do(func() {
		prometheus.MustRegister(dcCurrentGauge)
		prometheus.MustRegister(dcPowerGauge)
		prometheus.MustRegister(dcVoltageGauge)
		prometheus.MustRegister(dcStateGauge)
	})
}

// updateDCMetrics updates the Prometheus metrics with current DC power state values
func updateDCMetrics(state DCPowerState) {
	dcCurrentGauge.Set(state.Current)
	dcPowerGauge.Set(state.Power)
	dcVoltageGauge.Set(state.Voltage)
	if state.IsOn {
		dcStateGauge.Set(1)
	} else {
		dcStateGauge.Set(0)
	}
}
