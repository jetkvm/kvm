package audio

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Variables for process monitoring (using configuration)
var (
	// System constants
	maxCPUPercent     = GetConfig().MaxCPUPercent
	minCPUPercent     = GetConfig().MinCPUPercent
	defaultClockTicks = GetConfig().DefaultClockTicks
	defaultMemoryGB   = GetConfig().DefaultMemoryGB

	// Monitoring thresholds
	maxWarmupSamples = GetConfig().MaxWarmupSamples
	warmupCPUSamples = GetConfig().WarmupCPUSamples

	// Channel buffer size
	metricsChannelBuffer = GetConfig().MetricsChannelBuffer

	// Clock tick detection ranges
	minValidClockTicks = float64(GetConfig().MinValidClockTicks)
	maxValidClockTicks = float64(GetConfig().MaxValidClockTicks)
)

// Variables for process monitoring
var (
	pageSize = GetConfig().PageSize
)

// ProcessMetrics represents CPU and memory usage metrics for a process
type ProcessMetrics struct {
	PID           int       `json:"pid"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryRSS     int64     `json:"memory_rss_bytes"`
	MemoryVMS     int64     `json:"memory_vms_bytes"`
	MemoryPercent float64   `json:"memory_percent"`
	Timestamp     time.Time `json:"timestamp"`
	ProcessName   string    `json:"process_name"`
}

type ProcessMonitor struct {
	logger         zerolog.Logger
	mutex          sync.RWMutex
	monitoredPIDs  map[int]*processState
	running        bool
	stopChan       chan struct{}
	metricsChan    chan ProcessMetrics
	updateInterval time.Duration
	totalMemory    int64
	memoryOnce     sync.Once
	clockTicks     float64
	clockTicksOnce sync.Once
}

// processState tracks the state needed for CPU calculation
type processState struct {
	name          string
	lastCPUTime   int64
	lastSysTime   int64
	lastUserTime  int64
	lastSample    time.Time
	warmupSamples int
}

// NewProcessMonitor creates a new process monitor
func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{
		logger:         logging.GetDefaultLogger().With().Str("component", "process-monitor").Logger(),
		monitoredPIDs:  make(map[int]*processState),
		stopChan:       make(chan struct{}),
		metricsChan:    make(chan ProcessMetrics, metricsChannelBuffer),
		updateInterval: GetMetricsUpdateInterval(),
	}
}

// Start begins monitoring processes
func (pm *ProcessMonitor) Start() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.running {
		return
	}

	pm.running = true
	go pm.monitorLoop()
	pm.logger.Info().Msg("Process monitor started")
}

// Stop stops monitoring processes
func (pm *ProcessMonitor) Stop() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if !pm.running {
		return
	}

	pm.running = false
	close(pm.stopChan)
	pm.logger.Info().Msg("Process monitor stopped")
}

// AddProcess adds a process to monitor
func (pm *ProcessMonitor) AddProcess(pid int, name string) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.monitoredPIDs[pid] = &processState{
		name:       name,
		lastSample: time.Now(),
	}
	pm.logger.Info().Int("pid", pid).Str("name", name).Msg("Added process to monitor")
}

// RemoveProcess removes a process from monitoring
func (pm *ProcessMonitor) RemoveProcess(pid int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	delete(pm.monitoredPIDs, pid)
	pm.logger.Info().Int("pid", pid).Msg("Removed process from monitor")
}

// GetMetricsChan returns the channel for receiving metrics
func (pm *ProcessMonitor) GetMetricsChan() <-chan ProcessMetrics {
	return pm.metricsChan
}

// GetCurrentMetrics returns current metrics for all monitored processes
func (pm *ProcessMonitor) GetCurrentMetrics() []ProcessMetrics {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var metrics []ProcessMetrics
	for pid, state := range pm.monitoredPIDs {
		if metric, err := pm.collectMetrics(pid, state); err == nil {
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

// monitorLoop is the main monitoring loop
func (pm *ProcessMonitor) monitorLoop() {
	ticker := time.NewTicker(pm.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopChan:
			return
		case <-ticker.C:
			pm.collectAllMetrics()
		}
	}
}

func (pm *ProcessMonitor) collectAllMetrics() {
	pm.mutex.RLock()
	pidsToCheck := make([]int, 0, len(pm.monitoredPIDs))
	states := make([]*processState, 0, len(pm.monitoredPIDs))
	for pid, state := range pm.monitoredPIDs {
		pidsToCheck = append(pidsToCheck, pid)
		states = append(states, state)
	}
	pm.mutex.RUnlock()

	deadPIDs := make([]int, 0)
	for i, pid := range pidsToCheck {
		if metric, err := pm.collectMetrics(pid, states[i]); err == nil {
			select {
			case pm.metricsChan <- metric:
			default:
			}
		} else {
			deadPIDs = append(deadPIDs, pid)
		}
	}

	for _, pid := range deadPIDs {
		pm.RemoveProcess(pid)
	}
}

func (pm *ProcessMonitor) collectMetrics(pid int, state *processState) (ProcessMetrics, error) {
	now := time.Now()
	metric := ProcessMetrics{
		PID:         pid,
		Timestamp:   now,
		ProcessName: state.name,
	}

	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return metric, fmt.Errorf("failed to read process statistics from /proc/%d/stat: %w", pid, err)
	}

	fields := strings.Fields(string(statData))
	if len(fields) < 24 {
		return metric, fmt.Errorf("invalid process stat format: expected at least 24 fields, got %d from /proc/%d/stat", len(fields), pid)
	}

	utime, _ := strconv.ParseInt(fields[13], 10, 64)
	stime, _ := strconv.ParseInt(fields[14], 10, 64)
	totalCPUTime := utime + stime

	vsize, _ := strconv.ParseInt(fields[22], 10, 64)
	rss, _ := strconv.ParseInt(fields[23], 10, 64)

	metric.MemoryRSS = rss * int64(pageSize)
	metric.MemoryVMS = vsize

	// Calculate CPU percentage
	metric.CPUPercent = pm.calculateCPUPercent(totalCPUTime, state, now)

	// Increment warmup counter
	if state.warmupSamples < maxWarmupSamples {
		state.warmupSamples++
	}

	// Calculate memory percentage (RSS / total system memory)
	if totalMem := pm.getTotalMemory(); totalMem > 0 {
		metric.MemoryPercent = float64(metric.MemoryRSS) / float64(totalMem) * GetConfig().PercentageMultiplier
	}

	// Update state for next calculation
	state.lastCPUTime = totalCPUTime
	state.lastUserTime = utime
	state.lastSysTime = stime
	state.lastSample = now

	return metric, nil
}

// calculateCPUPercent calculates CPU percentage for a process with validation and bounds checking.
//
// Validation Rules:
//   - Returns 0.0 for first sample (no baseline for comparison)
//   - Requires positive time delta between samples
//   - Applies CPU percentage bounds: [MinCPUPercent, MaxCPUPercent]
//   - Uses system clock ticks for accurate CPU time conversion
//   - Validates clock ticks within range [MinValidClockTicks, MaxValidClockTicks]
//
// Bounds Applied:
//   - CPU percentage clamped to [0.01%, 100.0%] (default values)
//   - Clock ticks validated within [50, 1000] range (default values)
//   - Time delta must be > 0 to prevent division by zero
//
// Warmup Behavior:
//   - During warmup period (< WarmupCPUSamples), returns MinCPUPercent for idle processes
//   - This indicates process is alive but not consuming significant CPU
//
// The function ensures accurate CPU percentage calculation while preventing
// invalid measurements that could affect system monitoring and adaptive algorithms.
func (pm *ProcessMonitor) calculateCPUPercent(totalCPUTime int64, state *processState, now time.Time) float64 {
	if state.lastSample.IsZero() {
		// First sample - initialize baseline
		state.warmupSamples = 0
		return 0.0
	}

	timeDelta := now.Sub(state.lastSample).Seconds()
	cpuDelta := float64(totalCPUTime - state.lastCPUTime)

	if timeDelta <= 0 {
		return 0.0
	}

	if cpuDelta > 0 {
		// Convert from clock ticks to seconds using actual system clock ticks
		clockTicks := pm.getClockTicks()
		cpuSeconds := cpuDelta / clockTicks
		cpuPercent := (cpuSeconds / timeDelta) * GetConfig().PercentageMultiplier

		// Apply bounds
		if cpuPercent > maxCPUPercent {
			cpuPercent = maxCPUPercent
		}
		if cpuPercent < minCPUPercent {
			cpuPercent = minCPUPercent
		}

		return cpuPercent
	}

	// No CPU delta - process was idle
	if state.warmupSamples < warmupCPUSamples {
		// During warmup, provide a small non-zero value to indicate process is alive
		return minCPUPercent
	}

	return 0.0
}

func (pm *ProcessMonitor) getClockTicks() float64 {
	pm.clockTicksOnce.Do(func() {
		// Try to detect actual clock ticks from kernel boot parameters or /proc/stat
		if data, err := os.ReadFile("/proc/cmdline"); err == nil {
			// Look for HZ parameter in kernel command line
			cmdline := string(data)
			if strings.Contains(cmdline, "HZ=") {
				fields := strings.Fields(cmdline)
				for _, field := range fields {
					if strings.HasPrefix(field, "HZ=") {
						if hz, err := strconv.ParseFloat(field[3:], 64); err == nil && hz > 0 {
							pm.clockTicks = hz
							return
						}
					}
				}
			}
		}

		// Try reading from /proc/timer_list for more accurate detection
		if data, err := os.ReadFile("/proc/timer_list"); err == nil {
			timer := string(data)
			// Look for tick device frequency
			lines := strings.Split(timer, "\n")
			for _, line := range lines {
				if strings.Contains(line, "tick_period:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if period, err := strconv.ParseInt(fields[1], 10, 64); err == nil && period > 0 {
							// Convert nanoseconds to Hz
							hz := GetConfig().CGONanosecondsPerSecond / float64(period)
							if hz >= minValidClockTicks && hz <= maxValidClockTicks {
								pm.clockTicks = hz
								return
							}
						}
					}
				}
			}
		}

		// Fallback: Most embedded ARM systems (like jetKVM) use 250 Hz or 1000 Hz
		// rather than the traditional 100 Hz
		pm.clockTicks = defaultClockTicks
		pm.logger.Warn().Float64("clock_ticks", pm.clockTicks).Msg("Using fallback clock ticks value")

		// Log successful detection for non-fallback values
		if pm.clockTicks != defaultClockTicks {
			pm.logger.Info().Float64("clock_ticks", pm.clockTicks).Msg("Detected system clock ticks")
		}
	})
	return pm.clockTicks
}

func (pm *ProcessMonitor) getTotalMemory() int64 {
	pm.memoryOnce.Do(func() {
		file, err := os.Open("/proc/meminfo")
		if err != nil {
			pm.totalMemory = int64(defaultMemoryGB) * int64(GetConfig().ProcessMonitorKBToBytes) * int64(GetConfig().ProcessMonitorKBToBytes) * int64(GetConfig().ProcessMonitorKBToBytes)
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						pm.totalMemory = kb * int64(GetConfig().ProcessMonitorKBToBytes)
						return
					}
				}
				break
			}
		}
		pm.totalMemory = int64(defaultMemoryGB) * int64(GetConfig().ProcessMonitorKBToBytes) * int64(GetConfig().ProcessMonitorKBToBytes) * int64(GetConfig().ProcessMonitorKBToBytes) // Fallback
	})
	return pm.totalMemory
}

// GetTotalMemory returns total system memory in bytes (public method)
func (pm *ProcessMonitor) GetTotalMemory() int64 {
	return pm.getTotalMemory()
}

// Global process monitor instance
var globalProcessMonitor *ProcessMonitor
var processMonitorOnce sync.Once

// GetProcessMonitor returns the global process monitor instance
func GetProcessMonitor() *ProcessMonitor {
	processMonitorOnce.Do(func() {
		globalProcessMonitor = NewProcessMonitor()
		globalProcessMonitor.Start()
	})
	return globalProcessMonitor
}
