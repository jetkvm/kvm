# JetKVM Copilot Instructions

## Architecture Overview

JetKVM is a high-performance KVM-over-IP solution with **bidirectional architecture**: a Go backend running on ARM hardware and a React/TypeScript frontend served by the device. The system provides real-time video streaming, HID input, virtual media mounting, and **2-way audio support** via WebRTC.

### Core Components

- **Backend**: Go application (`main.go`, `web.go`, `webrtc.go`) handling WebRTC, HTTP APIs, hardware interfaces
- **Frontend**: React/TypeScript SPA (`ui/src/`) with Vite build system, served as embedded static files
- **Hardware Layer**: CGO bridge (`internal/native/`) to ARM SoC for video capture, USB gadget, display control
- **Audio System**: In-process CGO implementation (`internal/audio/`) with ALSA/Opus for USB Audio Gadget streaming
- **USB Gadget**: Configurable emulation (`internal/usbgadget/`) - keyboard, mouse, mass storage, **audio device**

## Development Workflows

### Quick Development Commands
```bash
# Full development deployment (most common)
./dev_deploy.sh -r <DEVICE_IP>

# UI-only changes (faster iteration)
cd ui && ./dev_device.sh <DEVICE_IP>

# Backend-only changes (skip UI build)
./dev_deploy.sh -r <DEVICE_IP> --skip-ui-build

# Build for release
make build_release
```

### Critical Build Dependencies
- **Audio libraries**: `make build_audio_deps` builds ALSA and Opus static libs for ARM
- **Native CGO**: `make build_native` compiles hardware interface layer
- **Cross-compilation**: ARM7 with buildkit toolchain at `/opt/jetkvm-native-buildkit`

### Testing
```bash
# Run Go tests on device
./dev_deploy.sh -r <DEVICE_IP> --run-go-tests

# UI linting
cd ui && npm run lint
```

## Project-Specific Patterns

### Configuration Management
- **Single config file**: `/userdata/kvm_config.json` persisted across updates
- **Config struct**: `config.go` with JSON tags, atomic operations for thread safety
- **Migration pattern**: Use `ensureConfigLoaded()` before config access

### WebRTC Architecture
- **Session management**: `webrtc.go` handles peer connections with connection pooling
- **Media tracks**: Separate video and **stereo audio tracks** in independent MediaStreams
- **Data channels**: RPC (ordered), HID (unordered), upload, terminal, serial channels
- **SDP munging**: Frontend modifies SDP for Opus stereo parameters (`OPUS_STEREO_PARAMS`)

### Audio Implementation Details
- **USB Audio Gadget**: UAC1 device presenting as stereo speakers + microphone
- **Bidirectional streaming**: Output (device→browser) and input (browser→device) 
- **CGO integration**: `internal/audio/cgo_source.go` bridges Go↔C for ALSA operations
- **Frame-based processing**: 20ms Opus frames (960 samples @ 48kHz) for low latency

### Frontend State Management
- **Zustand stores**: `hooks/stores.ts` - RTC, UI, Settings, Video state with persistence
- **JSON-RPC communication**: `useJsonRpc` hook for backend API calls
- **Localization**: Paraglide.js with compile-time validation - all UI strings use `m.key()` pattern

### USB Gadget Configuration
- **Configfs-based**: Dynamic reconfiguration via `/sys/kernel/config/usb_gadget/`
- **Transaction pattern**: `WithTransaction()` ensures atomic gadget changes
- **Device composition**: Keyboard + mouse + mass storage + **audio** with individual toggle support

## Integration Patterns

### Backend API Structure
- **JSON-RPC over WebSocket**: Primary communication via data channels
- **HTTP endpoints**: `/api/*` for REST operations, `/static/*` for UI assets
- **Configuration endpoints**: `rpc*` functions in `jsonrpc.go` with parameter validation

### Cross-Component Communication
- **Video pipeline**: `native.go` → WebRTC track → browser via H.264 streaming
- **HID input**: Browser → WebRTC data channel → `internal/usbgadget/hid_*.go` → Linux gadget
- **Audio pipeline**: ALSA capture → Opus encode → WebRTC → browser speakers (and reverse for mic)
- **Virtual media**: HTTP upload → storage → NBD server → USB mass storage gadget

### Error Handling Patterns
- **Graceful degradation**: Audio/video failures don't crash core functionality
- **Session isolation**: Per-connection logging with `connectionID` context
- **Recovery mechanisms**: USB gadget timeouts, audio restart on device changes

## Key Files for Common Tasks

### Adding new RPC endpoints
1. Add handler function in `jsonrpc.go` 
2. Register in `rpcHandlers` map
3. Add frontend hook in `useJsonRpc`

### USB device modifications
- `internal/usbgadget/config.go` - gadget composition and attributes
- `internal/usbgadget/hid_*.go` - HID device implementations

### Audio feature development
- `audio.go` - high-level audio management and session integration
- `internal/audio/` - CGO sources, relays, and C audio processing

### Frontend feature development
- `ui/src/routes/` - page components
- `ui/src/components/` - reusable UI components  
- `ui/localization/messages/en.json` - localizable strings

---

*This codebase uses advanced patterns like CGO, USB gadgets, and real-time media streaming. Focus on understanding the session lifecycle and cross-compilation requirements for effective development.*