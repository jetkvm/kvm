package diagnostics

import "runtime"

// LogSystemInfo logs uptime info.
func (d *Diagnostics) LogSystemInfo() {
	d.logger.Info().Msg("--- System Info ---")

	// Uptime
	d.runCmdLog("uptime", "uptime")
}

// LogGoRuntime logs Go runtime statistics.
func (d *Diagnostics) LogGoRuntime() {
	d.logger.Info().Msg("--- Go Runtime ---")

	// Goroutine count and GOMAXPROCS
	d.logger.Info().
		Int("goroutines", runtime.NumGoroutine()).
		Int("gomaxprocs", runtime.GOMAXPROCS(0)).
		Msg("Go runtime info")

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	d.logger.Info().
		Uint64("alloc_mb", m.Alloc/1024/1024).
		Uint64("total_alloc_mb", m.TotalAlloc/1024/1024).
		Uint64("sys_mb", m.Sys/1024/1024).
		Uint32("num_gc", m.NumGC).
		Uint64("heap_objects", m.HeapObjects).
		Msg("Go memory stats")
}
