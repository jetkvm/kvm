package audio

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// LatencyProfiler provides comprehensive end-to-end audio latency profiling
// with nanosecond precision across the entire WebRTC->IPC->CGO->ALSA pipeline
type LatencyProfiler struct {
	// Atomic counters for thread-safe access (MUST be first for ARM32 alignment)
	totalMeasurements       int64 // Total number of measurements taken
	webrtcLatencySum        int64 // Sum of WebRTC processing latencies (nanoseconds)
	ipcLatencySum           int64 // Sum of IPC communication latencies (nanoseconds)
	cgoLatencySum           int64 // Sum of CGO call latencies (nanoseconds)
	alsaLatencySum          int64 // Sum of ALSA device latencies (nanoseconds)
	endToEndLatencySum      int64 // Sum of complete end-to-end latencies (nanoseconds)
	validationLatencySum    int64 // Sum of validation overhead (nanoseconds)
	serializationLatencySum int64 // Sum of serialization overhead (nanoseconds)

	// Peak latency tracking
	maxWebrtcLatency   int64 // Maximum WebRTC latency observed (nanoseconds)
	maxIpcLatency      int64 // Maximum IPC latency observed (nanoseconds)
	maxCgoLatency      int64 // Maximum CGO latency observed (nanoseconds)
	maxAlsaLatency     int64 // Maximum ALSA latency observed (nanoseconds)
	maxEndToEndLatency int64 // Maximum end-to-end latency observed (nanoseconds)

	// Configuration and control
	config  LatencyProfilerConfig
	logger  zerolog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	running int32 // Atomic flag for profiler state
	enabled int32 // Atomic flag for measurement collection

	// Detailed measurement storage
	measurements     []DetailedLatencyMeasurement
	measurementMutex sync.RWMutex
	measurementIndex int

	// High-resolution timing
	timeSource func() int64 // Nanosecond precision time source
}

// LatencyProfilerConfig defines profiler configuration
type LatencyProfilerConfig struct {
	MaxMeasurements     int           // Maximum measurements to store in memory
	SamplingRate        float64       // Sampling rate (0.0-1.0, 1.0 = profile every frame)
	ReportingInterval   time.Duration // How often to log profiling reports
	ThresholdWarning    time.Duration // Latency threshold for warnings
	ThresholdCritical   time.Duration // Latency threshold for critical alerts
	EnableDetailedTrace bool          // Enable detailed per-component tracing
	EnableHistogram     bool          // Enable latency histogram collection
}

// DetailedLatencyMeasurement captures comprehensive latency breakdown
type DetailedLatencyMeasurement struct {
	Timestamp            time.Time     // When the measurement was taken
	FrameID              uint64        // Unique frame identifier for tracing
	WebRTCLatency        time.Duration // WebRTC processing time
	IPCLatency           time.Duration // IPC communication time
	CGOLatency           time.Duration // CGO call overhead
	ALSALatency          time.Duration // ALSA device processing time
	ValidationLatency    time.Duration // Frame validation overhead
	SerializationLatency time.Duration // Data serialization overhead
	EndToEndLatency      time.Duration // Complete pipeline latency
	Source               string        // Source component (input/output)
	FrameSize            int           // Size of the audio frame in bytes
	CPUUsage             float64       // CPU usage at time of measurement
	MemoryUsage          uint64        // Memory usage at time of measurement
}

// LatencyProfileReport contains aggregated profiling results
type LatencyProfileReport struct {
	TotalMeasurements int64         // Total measurements taken
	TimeRange         time.Duration // Time span of measurements

	// Average latencies
	AvgWebRTCLatency        time.Duration
	AvgIPCLatency           time.Duration
	AvgCGOLatency           time.Duration
	AvgALSALatency          time.Duration
	AvgEndToEndLatency      time.Duration
	AvgValidationLatency    time.Duration
	AvgSerializationLatency time.Duration

	// Peak latencies
	MaxWebRTCLatency   time.Duration
	MaxIPCLatency      time.Duration
	MaxCGOLatency      time.Duration
	MaxALSALatency     time.Duration
	MaxEndToEndLatency time.Duration

	// Performance analysis
	BottleneckComponent string         // Component with highest average latency
	LatencyDistribution map[string]int // Histogram of latency ranges
	Throughput          float64        // Frames per second processed
}

// FrameLatencyTracker tracks latency for a single audio frame through the pipeline
type FrameLatencyTracker struct {
	frameID                uint64
	startTime              int64 // Nanosecond timestamp
	webrtcStartTime        int64
	ipcStartTime           int64
	cgoStartTime           int64
	alsaStartTime          int64
	validationStartTime    int64
	serializationStartTime int64
	frameSize              int
	source                 string
}

// Global profiler instance
var (
	globalLatencyProfiler unsafe.Pointer // *LatencyProfiler
	profilerInitialized   int32
)

// DefaultLatencyProfilerConfig returns default profiler configuration
func DefaultLatencyProfilerConfig() LatencyProfilerConfig {
	config := GetConfig()
	return LatencyProfilerConfig{
		MaxMeasurements:     10000,
		SamplingRate:        config.LatencyProfilingSamplingRate, // Use configurable sampling rate
		ReportingInterval:   30 * time.Second,
		ThresholdWarning:    50 * time.Millisecond,
		ThresholdCritical:   100 * time.Millisecond,
		EnableDetailedTrace: false,                         // Disabled by default for performance
		EnableHistogram:     config.EnableLatencyProfiling, // Only enable if profiling is enabled
	}
}

// NewLatencyProfiler creates a new latency profiler
func NewLatencyProfiler(config LatencyProfilerConfig) *LatencyProfiler {
	ctx, cancel := context.WithCancel(context.Background())
	logger := logging.GetDefaultLogger().With().Str("component", "latency-profiler").Logger()

	// Validate configuration
	if config.MaxMeasurements <= 0 {
		config.MaxMeasurements = 10000
	}
	if config.SamplingRate < 0.0 || config.SamplingRate > 1.0 {
		config.SamplingRate = 0.1
	}
	if config.ReportingInterval <= 0 {
		config.ReportingInterval = 30 * time.Second
	}

	profiler := &LatencyProfiler{
		config:       config,
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		measurements: make([]DetailedLatencyMeasurement, config.MaxMeasurements),
		timeSource:   func() int64 { return time.Now().UnixNano() },
	}

	// Initialize peak latencies to zero
	atomic.StoreInt64(&profiler.maxWebrtcLatency, 0)
	atomic.StoreInt64(&profiler.maxIpcLatency, 0)
	atomic.StoreInt64(&profiler.maxCgoLatency, 0)
	atomic.StoreInt64(&profiler.maxAlsaLatency, 0)
	atomic.StoreInt64(&profiler.maxEndToEndLatency, 0)

	return profiler
}

// Start begins latency profiling
func (lp *LatencyProfiler) Start() error {
	if !atomic.CompareAndSwapInt32(&lp.running, 0, 1) {
		return fmt.Errorf("latency profiler already running")
	}

	// Enable measurement collection
	atomic.StoreInt32(&lp.enabled, 1)

	// Start reporting goroutine
	go lp.reportingLoop()

	lp.logger.Info().Float64("sampling_rate", lp.config.SamplingRate).Msg("latency profiler started")
	return nil
}

// Stop stops latency profiling
func (lp *LatencyProfiler) Stop() {
	if !atomic.CompareAndSwapInt32(&lp.running, 1, 0) {
		return
	}

	// Disable measurement collection
	atomic.StoreInt32(&lp.enabled, 0)

	// Cancel context to stop reporting
	lp.cancel()

	lp.logger.Info().Msg("latency profiler stopped")
}

// IsEnabled returns whether profiling is currently enabled
func (lp *LatencyProfiler) IsEnabled() bool {
	return atomic.LoadInt32(&lp.enabled) == 1
}

// StartFrameTracking begins tracking latency for a new audio frame
func (lp *LatencyProfiler) StartFrameTracking(frameID uint64, frameSize int, source string) *FrameLatencyTracker {
	if !lp.IsEnabled() {
		return nil
	}

	// Apply sampling rate to reduce profiling overhead
	if lp.config.SamplingRate < 1.0 {
		// Simple sampling based on frame ID
		if float64(frameID%100)/100.0 > lp.config.SamplingRate {
			return nil
		}
	}

	now := lp.timeSource()
	return &FrameLatencyTracker{
		frameID:   frameID,
		startTime: now,
		frameSize: frameSize,
		source:    source,
	}
}

// TrackWebRTCStart marks the start of WebRTC processing
func (tracker *FrameLatencyTracker) TrackWebRTCStart() {
	if tracker != nil {
		tracker.webrtcStartTime = time.Now().UnixNano()
	}
}

// TrackIPCStart marks the start of IPC communication
func (tracker *FrameLatencyTracker) TrackIPCStart() {
	if tracker != nil {
		tracker.ipcStartTime = time.Now().UnixNano()
	}
}

// TrackCGOStart marks the start of CGO processing
func (tracker *FrameLatencyTracker) TrackCGOStart() {
	if tracker != nil {
		tracker.cgoStartTime = time.Now().UnixNano()
	}
}

// TrackALSAStart marks the start of ALSA device processing
func (tracker *FrameLatencyTracker) TrackALSAStart() {
	if tracker != nil {
		tracker.alsaStartTime = time.Now().UnixNano()
	}
}

// TrackValidationStart marks the start of frame validation
func (tracker *FrameLatencyTracker) TrackValidationStart() {
	if tracker != nil {
		tracker.validationStartTime = time.Now().UnixNano()
	}
}

// TrackSerializationStart marks the start of data serialization
func (tracker *FrameLatencyTracker) TrackSerializationStart() {
	if tracker != nil {
		tracker.serializationStartTime = time.Now().UnixNano()
	}
}

// FinishTracking completes frame tracking and records the measurement
func (lp *LatencyProfiler) FinishTracking(tracker *FrameLatencyTracker) {
	if tracker == nil || !lp.IsEnabled() {
		return
	}

	endTime := lp.timeSource()

	// Calculate component latencies
	var webrtcLatency, ipcLatency, cgoLatency, alsaLatency, validationLatency, serializationLatency time.Duration

	if tracker.webrtcStartTime > 0 {
		webrtcLatency = time.Duration(tracker.ipcStartTime - tracker.webrtcStartTime)
	}
	if tracker.ipcStartTime > 0 {
		ipcLatency = time.Duration(tracker.cgoStartTime - tracker.ipcStartTime)
	}
	if tracker.cgoStartTime > 0 {
		cgoLatency = time.Duration(tracker.alsaStartTime - tracker.cgoStartTime)
	}
	if tracker.alsaStartTime > 0 {
		alsaLatency = time.Duration(endTime - tracker.alsaStartTime)
	}
	if tracker.validationStartTime > 0 {
		validationLatency = time.Duration(tracker.ipcStartTime - tracker.validationStartTime)
	}
	if tracker.serializationStartTime > 0 {
		serializationLatency = time.Duration(tracker.cgoStartTime - tracker.serializationStartTime)
	}

	endToEndLatency := time.Duration(endTime - tracker.startTime)

	// Update atomic counters
	atomic.AddInt64(&lp.totalMeasurements, 1)
	atomic.AddInt64(&lp.webrtcLatencySum, webrtcLatency.Nanoseconds())
	atomic.AddInt64(&lp.ipcLatencySum, ipcLatency.Nanoseconds())
	atomic.AddInt64(&lp.cgoLatencySum, cgoLatency.Nanoseconds())
	atomic.AddInt64(&lp.alsaLatencySum, alsaLatency.Nanoseconds())
	atomic.AddInt64(&lp.endToEndLatencySum, endToEndLatency.Nanoseconds())
	atomic.AddInt64(&lp.validationLatencySum, validationLatency.Nanoseconds())
	atomic.AddInt64(&lp.serializationLatencySum, serializationLatency.Nanoseconds())

	// Update peak latencies
	lp.updatePeakLatency(&lp.maxWebrtcLatency, webrtcLatency.Nanoseconds())
	lp.updatePeakLatency(&lp.maxIpcLatency, ipcLatency.Nanoseconds())
	lp.updatePeakLatency(&lp.maxCgoLatency, cgoLatency.Nanoseconds())
	lp.updatePeakLatency(&lp.maxAlsaLatency, alsaLatency.Nanoseconds())
	lp.updatePeakLatency(&lp.maxEndToEndLatency, endToEndLatency.Nanoseconds())

	// Store detailed measurement if enabled
	if lp.config.EnableDetailedTrace {
		lp.storeMeasurement(DetailedLatencyMeasurement{
			Timestamp:            time.Now(),
			FrameID:              tracker.frameID,
			WebRTCLatency:        webrtcLatency,
			IPCLatency:           ipcLatency,
			CGOLatency:           cgoLatency,
			ALSALatency:          alsaLatency,
			ValidationLatency:    validationLatency,
			SerializationLatency: serializationLatency,
			EndToEndLatency:      endToEndLatency,
			Source:               tracker.source,
			FrameSize:            tracker.frameSize,
			CPUUsage:             lp.getCurrentCPUUsage(),
			MemoryUsage:          lp.getCurrentMemoryUsage(),
		})
	}

	// Check for threshold violations
	if endToEndLatency > lp.config.ThresholdCritical {
		lp.logger.Error().Dur("latency", endToEndLatency).Uint64("frame_id", tracker.frameID).
			Str("source", tracker.source).Msg("critical latency threshold exceeded")
	} else if endToEndLatency > lp.config.ThresholdWarning {
		lp.logger.Warn().Dur("latency", endToEndLatency).Uint64("frame_id", tracker.frameID).
			Str("source", tracker.source).Msg("warning latency threshold exceeded")
	}
}

// updatePeakLatency atomically updates peak latency if new value is higher
func (lp *LatencyProfiler) updatePeakLatency(peakPtr *int64, newLatency int64) {
	for {
		current := atomic.LoadInt64(peakPtr)
		if newLatency <= current || atomic.CompareAndSwapInt64(peakPtr, current, newLatency) {
			break
		}
	}
}

// storeMeasurement stores a detailed measurement in the circular buffer
func (lp *LatencyProfiler) storeMeasurement(measurement DetailedLatencyMeasurement) {
	lp.measurementMutex.Lock()
	defer lp.measurementMutex.Unlock()

	lp.measurements[lp.measurementIndex] = measurement
	lp.measurementIndex = (lp.measurementIndex + 1) % len(lp.measurements)
}

// GetReport generates a comprehensive latency profiling report
func (lp *LatencyProfiler) GetReport() LatencyProfileReport {
	totalMeasurements := atomic.LoadInt64(&lp.totalMeasurements)
	if totalMeasurements == 0 {
		return LatencyProfileReport{}
	}

	// Calculate averages
	avgWebRTC := time.Duration(atomic.LoadInt64(&lp.webrtcLatencySum) / totalMeasurements)
	avgIPC := time.Duration(atomic.LoadInt64(&lp.ipcLatencySum) / totalMeasurements)
	avgCGO := time.Duration(atomic.LoadInt64(&lp.cgoLatencySum) / totalMeasurements)
	avgALSA := time.Duration(atomic.LoadInt64(&lp.alsaLatencySum) / totalMeasurements)
	avgEndToEnd := time.Duration(atomic.LoadInt64(&lp.endToEndLatencySum) / totalMeasurements)
	avgValidation := time.Duration(atomic.LoadInt64(&lp.validationLatencySum) / totalMeasurements)
	avgSerialization := time.Duration(atomic.LoadInt64(&lp.serializationLatencySum) / totalMeasurements)

	// Get peak latencies
	maxWebRTC := time.Duration(atomic.LoadInt64(&lp.maxWebrtcLatency))
	maxIPC := time.Duration(atomic.LoadInt64(&lp.maxIpcLatency))
	maxCGO := time.Duration(atomic.LoadInt64(&lp.maxCgoLatency))
	maxALSA := time.Duration(atomic.LoadInt64(&lp.maxAlsaLatency))
	maxEndToEnd := time.Duration(atomic.LoadInt64(&lp.maxEndToEndLatency))

	// Determine bottleneck component
	bottleneck := "WebRTC"
	maxAvg := avgWebRTC
	if avgIPC > maxAvg {
		bottleneck = "IPC"
		maxAvg = avgIPC
	}
	if avgCGO > maxAvg {
		bottleneck = "CGO"
		maxAvg = avgCGO
	}
	if avgALSA > maxAvg {
		bottleneck = "ALSA"
	}

	return LatencyProfileReport{
		TotalMeasurements:       totalMeasurements,
		AvgWebRTCLatency:        avgWebRTC,
		AvgIPCLatency:           avgIPC,
		AvgCGOLatency:           avgCGO,
		AvgALSALatency:          avgALSA,
		AvgEndToEndLatency:      avgEndToEnd,
		AvgValidationLatency:    avgValidation,
		AvgSerializationLatency: avgSerialization,
		MaxWebRTCLatency:        maxWebRTC,
		MaxIPCLatency:           maxIPC,
		MaxCGOLatency:           maxCGO,
		MaxALSALatency:          maxALSA,
		MaxEndToEndLatency:      maxEndToEnd,
		BottleneckComponent:     bottleneck,
	}
}

// reportingLoop periodically logs profiling reports
func (lp *LatencyProfiler) reportingLoop() {
	ticker := time.NewTicker(lp.config.ReportingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lp.ctx.Done():
			return
		case <-ticker.C:
			report := lp.GetReport()
			if report.TotalMeasurements > 0 {
				lp.logReport(report)
			}
		}
	}
}

// logReport logs a comprehensive profiling report
func (lp *LatencyProfiler) logReport(report LatencyProfileReport) {
	lp.logger.Info().
		Int64("total_measurements", report.TotalMeasurements).
		Dur("avg_webrtc_latency", report.AvgWebRTCLatency).
		Dur("avg_ipc_latency", report.AvgIPCLatency).
		Dur("avg_cgo_latency", report.AvgCGOLatency).
		Dur("avg_alsa_latency", report.AvgALSALatency).
		Dur("avg_end_to_end_latency", report.AvgEndToEndLatency).
		Dur("avg_validation_latency", report.AvgValidationLatency).
		Dur("avg_serialization_latency", report.AvgSerializationLatency).
		Dur("max_webrtc_latency", report.MaxWebRTCLatency).
		Dur("max_ipc_latency", report.MaxIPCLatency).
		Dur("max_cgo_latency", report.MaxCGOLatency).
		Dur("max_alsa_latency", report.MaxALSALatency).
		Dur("max_end_to_end_latency", report.MaxEndToEndLatency).
		Str("bottleneck_component", report.BottleneckComponent).
		Msg("latency profiling report")
}

// getCurrentCPUUsage returns current CPU usage percentage
func (lp *LatencyProfiler) getCurrentCPUUsage() float64 {
	// Simplified CPU usage - in production, this would use more sophisticated monitoring
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(runtime.NumGoroutine()) / 100.0 // Rough approximation
}

// getCurrentMemoryUsage returns current memory usage in bytes
func (lp *LatencyProfiler) getCurrentMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// GetGlobalLatencyProfiler returns the global latency profiler instance
func GetGlobalLatencyProfiler() *LatencyProfiler {
	ptr := atomic.LoadPointer(&globalLatencyProfiler)
	if ptr != nil {
		return (*LatencyProfiler)(ptr)
	}

	// Initialize on first use
	if atomic.CompareAndSwapInt32(&profilerInitialized, 0, 1) {
		config := DefaultLatencyProfilerConfig()
		profiler := NewLatencyProfiler(config)
		atomic.StorePointer(&globalLatencyProfiler, unsafe.Pointer(profiler))
		return profiler
	}

	// Another goroutine initialized it, try again
	ptr = atomic.LoadPointer(&globalLatencyProfiler)
	if ptr != nil {
		return (*LatencyProfiler)(ptr)
	}

	// Fallback: create a new profiler
	config := DefaultLatencyProfilerConfig()
	return NewLatencyProfiler(config)
}

// EnableLatencyProfiling enables the global latency profiler
func EnableLatencyProfiling() error {
	config := GetConfig()
	if !config.EnableLatencyProfiling {
		return fmt.Errorf("latency profiling is disabled in configuration")
	}
	profiler := GetGlobalLatencyProfiler()
	return profiler.Start()
}

// DisableLatencyProfiling disables the global latency profiler
func DisableLatencyProfiling() {
	ptr := atomic.LoadPointer(&globalLatencyProfiler)
	if ptr != nil {
		profiler := (*LatencyProfiler)(ptr)
		profiler.Stop()
	}
}

// ProfileFrameLatency is a convenience function to profile a single frame's latency
func ProfileFrameLatency(frameID uint64, frameSize int, source string, fn func(*FrameLatencyTracker)) {
	config := GetConfig()
	if !config.EnableLatencyProfiling {
		fn(nil)
		return
	}

	profiler := GetGlobalLatencyProfiler()
	if !profiler.IsEnabled() {
		fn(nil)
		return
	}

	tracker := profiler.StartFrameTracking(frameID, frameSize, source)
	defer profiler.FinishTracking(tracker)
	fn(tracker)
}
