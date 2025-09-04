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
   - AudioOutputSupervisor  (replaces: AudioOutputSupervisor) ✓
   - AudioOutputServer      (replaces: AudioOutputServer) ✓
   - AudioOutputClient      (replaces: AudioOutputClient) ✓

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
1. Missing AudioOutputIPCManager for symmetry
2. Missing OutputIPCConfig for consistency
3. Component names in logging should be standardized

IMPLEMENTATION PLAN:
1. Create AudioOutputIPCManager for symmetry
2. Standardize all component logging names
3. Update all references consistently
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
	AudioOutputSupervisorComponent = "audio-output-supervisor"
	AudioOutputServerComponent     = "audio-output-server"
	AudioOutputClientComponent     = "audio-output-client"
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
