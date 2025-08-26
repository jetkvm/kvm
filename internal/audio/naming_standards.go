package audio

import "time"

// Naming Standards Documentation
// This file documents the standardized naming conventions for audio components
// to ensure consistency across the entire audio system.

/*
STANDARDIZED NAMING CONVENTIONS:

1. COMPONENT HIERARCHY:
   - Manager: High-level component that orchestrates multiple subsystems
   - Supervisor: Process lifecycle management (start/stop/restart processes)
   - Server: IPC server that handles incoming connections
   - Client: IPC client that connects to servers
   - Streamer: High-performance streaming component

2. NAMING PATTERNS:
   Input Components:
   - AudioInputManager      (replaces: AudioInputManager) ✓
   - AudioInputSupervisor   (replaces: AudioInputSupervisor) ✓
   - AudioInputServer       (replaces: AudioInputServer) ✓
   - AudioInputClient       (replaces: AudioInputClient) ✓
   - AudioInputStreamer     (new: for consistency with OutputStreamer)

   Output Components:
   - AudioOutputManager     (new: missing high-level manager)
   - AudioOutputSupervisor  (replaces: AudioOutputSupervisor) ✓
   - AudioOutputServer      (replaces: AudioOutputServer) ✓
   - AudioOutputClient      (replaces: AudioOutputClient) ✓
   - AudioOutputStreamer    (replaces: OutputStreamer)

3. IPC NAMING:
   - AudioInputIPCManager   (replaces: AudioInputIPCManager) ✓
   - AudioOutputIPCManager  (new: for consistency)

4. CONFIGURATION NAMING:
   - InputIPCConfig         (replaces: InputIPCConfig) ✓
   - OutputIPCConfig        (new: for consistency)

5. MESSAGE NAMING:
   - InputIPCMessage        (replaces: InputIPCMessage) ✓
   - OutputIPCMessage       (replaces: OutputIPCMessage) ✓
   - InputMessageType       (replaces: InputMessageType) ✓
   - OutputMessageType      (replaces: OutputMessageType) ✓

ISSUES IDENTIFIED:
1. Missing AudioOutputManager (high-level output management)
2. Inconsistent naming: OutputStreamer vs AudioInputSupervisor
3. Missing AudioOutputIPCManager for symmetry
4. Missing OutputIPCConfig for consistency
5. Component names in logging should be standardized

IMPLEMENTATION PLAN:
1. Create AudioOutputManager to match AudioInputManager
2. Rename OutputStreamer to AudioOutputStreamer
3. Create AudioOutputIPCManager for symmetry
4. Standardize all component logging names
5. Update all references consistently
*/

// Component name constants for consistent logging
const (
	// Input component names
	AudioInputManagerComponent    = "audio-input-manager"
	AudioInputSupervisorComponent = "audio-input-supervisor"
	AudioInputServerComponent     = "audio-input-server"
	AudioInputClientComponent     = "audio-input-client"
	AudioInputIPCComponent        = "audio-input-ipc"

	// Output component names
	AudioOutputManagerComponent    = "audio-output-manager"
	AudioOutputSupervisorComponent = "audio-output-supervisor"
	AudioOutputServerComponent     = "audio-output-server"
	AudioOutputClientComponent     = "audio-output-client"
	AudioOutputStreamerComponent   = "audio-output-streamer"
	AudioOutputIPCComponent        = "audio-output-ipc"

	// Common component names
	AudioRelayComponent   = "audio-relay"
	AudioEventsComponent  = "audio-events"
	AudioMetricsComponent = "audio-metrics"
)

// Interface definitions for consistent component behavior
type AudioManagerInterface interface {
	Start() error
	Stop()
	IsRunning() bool
	IsReady() bool
	GetMetrics() interface{}
}

type AudioSupervisorInterface interface {
	Start() error
	Stop() error
	IsRunning() bool
	GetProcessPID() int
	GetProcessMetrics() *ProcessMetrics
}

type AudioServerInterface interface {
	Start() error
	Stop()
	Close() error
}

type AudioClientInterface interface {
	Connect() error
	Disconnect()
	IsConnected() bool
	Close() error
}

type AudioStreamerInterface interface {
	Start() error
	Stop()
	GetStats() (processed, dropped int64, avgProcessingTime time.Duration)
}
