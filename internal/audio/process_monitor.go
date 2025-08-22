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

// ProcessMonitor monitors CPU and memory usage of processes
type ProcessMonitor struct {
	logger       zerolog.Logger
	mutex        sync.RWMutex
	monitoredPIDs map[int]*processState
	running      bool
	stopChan     chan struct{}
	metricsChan  chan ProcessMetrics
	updateInterval time.Duration
}

// processState tracks the state needed for CPU calculation
type processState struct {
	name         string
	lastCPUTime  int64
	lastSysTime  int64
	lastUserTime int64
	lastSample   time.Time
}

// NewProcessMonitor creates a new process monitor
func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{
		logger:         logging.GetDefaultLogger().With().Str("component", "process-monitor").Logger(),
		monitoredPIDs:  make(map[int]*processState),
		stopChan:       make(chan struct{}),
		metricsChan:    make(chan ProcessMetrics, 100),
		updateInterval: 2 * time.Second, // Update every 2 seconds
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

// collectAllMetrics collects metrics for all monitored processes
func (pm *ProcessMonitor) collectAllMetrics() {
	pm.mutex.RLock()
	pids := make(map[int]*processState)
	for pid, state := range pm.monitoredPIDs {
		pids[pid] = state
	}
	pm.mutex.RUnlock()

	for pid, state := range pids {
		if metric, err := pm.collectMetrics(pid, state); err == nil {
			select {
			case pm.metricsChan <- metric:
			default:
				// Channel full, skip this metric
			}
		} else {
			// Process might have died, remove it
			pm.RemoveProcess(pid)
		}
	}
}

// collectMetrics collects metrics for a specific process
func (pm *ProcessMonitor) collectMetrics(pid int, state *processState) (ProcessMetrics, error) {
	now := time.Now()
	metric := ProcessMetrics{
		PID:         pid,
		Timestamp:   now,
		ProcessName: state.name,
	}

	// Read /proc/[pid]/stat for CPU and memory info
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return metric, fmt.Errorf("failed to read stat file: %w", err)
	}

	// Parse stat file
	fields := strings.Fields(string(statData))
	if len(fields) < 24 {
		return metric, fmt.Errorf("invalid stat file format")
	}

	// Extract CPU times (fields 13, 14 are utime, stime in clock ticks)
	utime, _ := strconv.ParseInt(fields[13], 10, 64)
	stime, _ := strconv.ParseInt(fields[14], 10, 64)
	totalCPUTime := utime + stime

	// Extract memory info (field 22 is vsize, field 23 is rss in pages)
	vsize, _ := strconv.ParseInt(fields[22], 10, 64)
	rss, _ := strconv.ParseInt(fields[23], 10, 64)

	// Convert RSS from pages to bytes (assuming 4KB pages)
	pageSize := int64(4096)
	metric.MemoryRSS = rss * pageSize
	metric.MemoryVMS = vsize

	// Calculate CPU percentage
	if !state.lastSample.IsZero() {
		timeDelta := now.Sub(state.lastSample).Seconds()
		cpuDelta := float64(totalCPUTime - state.lastCPUTime)
		
		// Convert from clock ticks to seconds (assuming 100 Hz)
		clockTicks := 100.0
		cpuSeconds := cpuDelta / clockTicks
		
		if timeDelta > 0 {
			metric.CPUPercent = (cpuSeconds / timeDelta) * 100.0
		}
	}

	// Calculate memory percentage (RSS / total system memory)
	if totalMem := pm.getTotalMemory(); totalMem > 0 {
		metric.MemoryPercent = float64(metric.MemoryRSS) / float64(totalMem) * 100.0
	}

	// Update state for next calculation
	state.lastCPUTime = totalCPUTime
	state.lastUserTime = utime
	state.lastSysTime = stime
	state.lastSample = now

	return metric, nil
}

// getTotalMemory returns total system memory in bytes
func (pm *ProcessMonitor) getTotalMemory() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb * 1024 // Convert KB to bytes
				}
			}
			break
		}
	}
	return 0
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