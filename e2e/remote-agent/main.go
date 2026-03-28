// remote-agent is a lightweight daemon that runs on the remote host (the machine
// controlled by JetKVM). It monitors input events, USB devices, and mounts,
// exposing them via a simple HTTP API for e2e test verification.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Linux input event structure (64-bit)
type inputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

const inputEventSize = int(unsafe.Sizeof(inputEvent{}))

// Event types
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03
)

// Relative axes
const (
	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08
)

// Absolute axes
const (
	absX = 0x00
	absY = 0x01
)

// Key states
const (
	keyRelease = 0
	keyPress   = 1
	keyRepeat  = 2
)

// InputEvent is a recorded input event for the API.
type InputEvent struct {
	Time   int64  `json:"time_ms"`
	Type   string `json:"type"`             // "key_press", "key_release", "key_repeat", "mouse_move_rel", "mouse_move_abs", "mouse_button"
	Code   uint16 `json:"code"`             // Linux key code or axis
	Value  int32  `json:"value,omitempty"`   // Key: 0/1/2, Mouse: delta or position
	X      int32  `json:"x"`                 // Mouse X (for move events)
	Y      int32  `json:"y"`                 // Mouse Y (for move events)
	Device string `json:"device,omitempty"`  // Source device name
}

// USBDevice represents a USB device.
type USBDevice struct {
	Bus     string `json:"bus"`
	Device  string `json:"device"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

// MountInfo represents a mount point.
type MountInfo struct {
	Device     string `json:"device"`
	MountPoint string `json:"mount_point"`
	FSType     string `json:"fs_type"`
	Options    string `json:"options"`
}

// EventBuffer stores recent input events with thread safety.
type EventBuffer struct {
	mu     sync.Mutex
	events []InputEvent
	maxAge time.Duration
}

func newEventBuffer(maxAge time.Duration) *EventBuffer {
	return &EventBuffer{
		events: make([]InputEvent, 0, 1024),
		maxAge: maxAge,
	}
}

func (b *EventBuffer) Add(ev InputEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev)
	b.prune()
}

func (b *EventBuffer) GetAndClear() []InputEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()
	result := make([]InputEvent, len(b.events))
	copy(result, b.events)
	b.events = b.events[:0]
	return result
}

func (b *EventBuffer) Get() []InputEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune()
	result := make([]InputEvent, len(b.events))
	copy(result, b.events)
	return result
}

func (b *EventBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = b.events[:0]
}

func (b *EventBuffer) prune() {
	cutoff := time.Now().Add(-b.maxAge).UnixMilli()
	i := 0
	for i < len(b.events) && b.events[i].Time < cutoff {
		i++
	}
	if i > 0 {
		b.events = b.events[i:]
	}
}

// Agent holds the state of the remote agent.
type Agent struct {
	keyboardEvents *EventBuffer
	mouseEvents    *EventBuffer
	monitorMu      sync.Mutex
	absMouseState  struct {
		mu sync.Mutex
		x  int32
		y  int32
	}
}

func newAgent() *Agent {
	return &Agent{
		keyboardEvents: newEventBuffer(30 * time.Second),
		mouseEvents:    newEventBuffer(30 * time.Second),
	}
}

// monitorAllDevices continuously discovers and monitors JetKVM input devices.
// When devices disappear (e.g., USB gadget reconfiguration), it re-discovers and reconnects.
func (a *Agent) monitorAllDevices() {
	monitored := make(map[string]context.CancelFunc) // path -> cancel

	for {
		devices := discoverJetKVMDevices()

		// Start monitoring new devices
		for path, name := range devices {
			if _, exists := monitored[path]; exists {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			monitored[path] = cancel
			go func(p, n string) {
				a.monitorDevice(ctx, p, n)
				// When monitorDevice returns, remove from tracked set
				a.monitorMu.Lock()
				delete(monitored, p)
				a.monitorMu.Unlock()
			}(path, name)
		}

		// Clean up stale entries (devices that disappeared)
		for path, cancel := range monitored {
			if _, exists := devices[path]; !exists {
				cancel()
				delete(monitored, path)
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// monitorDevice reads input events from an evdev device file.
func (a *Agent) monitorDevice(ctx context.Context, path string, deviceName string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("WARNING: cannot open %s (%s): %v", path, deviceName, err)
		return
	}
	defer f.Close()

	log.Printf("Monitoring %s (%s)", path, deviceName)
	buf := make([]byte, inputEventSize*64)

	// Close file when context is cancelled to unblock Read
	go func() {
		<-ctx.Done()
		f.Close()
	}()

	for {
		n, err := f.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("Stopped monitoring %s (context cancelled)", path)
			} else {
				log.Printf("Error reading %s: %v (will reconnect)", path, err)
			}
			return
		}

		for offset := 0; offset+inputEventSize <= n; offset += inputEventSize {
			var ev inputEvent
			ev.TimeSec = int64(binary.LittleEndian.Uint64(buf[offset:]))
			ev.TimeUsec = int64(binary.LittleEndian.Uint64(buf[offset+8:]))
			ev.Type = binary.LittleEndian.Uint16(buf[offset+16:])
			ev.Code = binary.LittleEndian.Uint16(buf[offset+18:])
			ev.Value = int32(binary.LittleEndian.Uint32(buf[offset+20:]))

			a.processEvent(ev, deviceName)
		}
	}
}

func (a *Agent) processEvent(ev inputEvent, deviceName string) {
	nowMs := ev.TimeSec*1000 + ev.TimeUsec/1000

	switch ev.Type {
	case evKey:
		// Key codes < 256 are keyboard keys, >= 0x110 are mouse buttons
		var evType string
		switch ev.Value {
		case keyPress:
			evType = "key_press"
		case keyRelease:
			evType = "key_release"
		case keyRepeat:
			evType = "key_repeat"
		default:
			return
		}

		isMouse := ev.Code >= 0x110 && ev.Code <= 0x11f
		recorded := InputEvent{
			Time:   nowMs,
			Type:   evType,
			Code:   ev.Code,
			Value:  ev.Value,
			Device: deviceName,
		}
		if isMouse {
			recorded.Type = "mouse_button"
			a.mouseEvents.Add(recorded)
		} else {
			a.keyboardEvents.Add(recorded)
		}

	case evRel:
		recorded := InputEvent{
			Time:   nowMs,
			Type:   "mouse_move_rel",
			Code:   ev.Code,
			Value:  ev.Value,
			Device: deviceName,
		}
		if ev.Code == relX {
			recorded.X = ev.Value
		} else if ev.Code == relY {
			recorded.Y = ev.Value
		}
		a.mouseEvents.Add(recorded)

	case evAbs:
		if ev.Code == absX || ev.Code == absY {
			a.absMouseState.mu.Lock()
			if ev.Code == absX {
				a.absMouseState.x = ev.Value
			} else {
				a.absMouseState.y = ev.Value
			}
			x, y := a.absMouseState.x, a.absMouseState.y
			a.absMouseState.mu.Unlock()

			a.mouseEvents.Add(InputEvent{
				Time:   nowMs,
				Type:   "mouse_move_abs",
				Code:   ev.Code,
				Value:  ev.Value,
				X:      x,
				Y:      y,
				Device: deviceName,
			})
		}
	}
}

// discoverJetKVMDevices finds input devices associated with JetKVM.
func discoverJetKVMDevices() map[string]string {
	devices := make(map[string]string)
	for _, dev := range listInputDevices() {
		if !dev.IsJetKVM {
			continue
		}
		label := dev.Name
		switch dev.Type {
		case "absolute_mouse":
			label += " (absolute mouse)"
		case "relative_mouse":
			label += " (relative mouse)"
		case "keyboard":
			label += " (keyboard)"
		}
		devices[dev.Path] = label
	}
	return devices
}

// listUSBDevices returns currently connected USB devices.
func listUSBDevices() []USBDevice {
	var devices []USBDevice

	entries, err := filepath.Glob("/sys/bus/usb/devices/[0-9]*")
	if err != nil {
		return devices
	}

	for _, entry := range entries {
		vendor := readSysFile(filepath.Join(entry, "idVendor"))
		product := readSysFile(filepath.Join(entry, "idProduct"))
		manufacturer := readSysFile(filepath.Join(entry, "manufacturer"))
		productName := readSysFile(filepath.Join(entry, "product"))

		if vendor == "" || product == "" {
			continue
		}

		name := productName
		if manufacturer != "" && productName != "" {
			name = manufacturer + " " + productName
		}

		devices = append(devices, USBDevice{
			Bus:    filepath.Base(entry),
			ID:     vendor + ":" + product,
			Name:   name,
		})
	}

	return devices
}

func readSysFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// InputDeviceInfo represents an input device visible in /proc/bus/input/devices.
type InputDeviceInfo struct {
	Name     string `json:"name"`
	Handler  string `json:"handler"`
	Path     string `json:"path"`
	Type     string `json:"type"` // "keyboard", "absolute_mouse", "relative_mouse", "unknown"
	IsJetKVM bool   `json:"is_jetkvm"`
}

// DisplayInfo represents display/monitor information.
type DisplayInfo struct {
	Connector  string `json:"connector"`
	Status     string `json:"status"` // "connected" or "disconnected"
	Resolution string `json:"resolution,omitempty"`
	Modes      []string `json:"modes,omitempty"`
}

// listInputDevices returns all input devices, with JetKVM ones flagged.
func listInputDevices() []InputDeviceInfo {
	var devices []InputDeviceInfo

	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return devices
	}

	for _, block := range strings.Split(string(data), "\n\n") {
		var name, handler string
		var hasKbd, hasMouse, hasAbs, hasRel bool
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "N: Name=") {
				name = strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
			}
			if strings.HasPrefix(line, "H: Handlers=") {
				parts := strings.Fields(strings.TrimPrefix(line, "H: Handlers="))
				for _, p := range parts {
					if strings.HasPrefix(p, "event") {
						handler = p
					}
					if p == "kbd" {
						hasKbd = true
					}
					if strings.HasPrefix(p, "mouse") {
						hasMouse = true
					}
				}
			}
			if strings.HasPrefix(line, "B: ABS=") && line != "B: ABS=0" {
				hasAbs = true
			}
			if strings.HasPrefix(line, "B: REL=") && line != "B: REL=0" {
				hasRel = true
			}
		}
		if handler == "" {
			continue
		}

		devType := "unknown"
		if hasMouse && hasAbs {
			devType = "absolute_mouse"
		} else if hasMouse && hasRel {
			devType = "relative_mouse"
		} else if hasKbd {
			devType = "keyboard"
		}

		devices = append(devices, InputDeviceInfo{
			Name:     name,
			Handler:  handler,
			Path:     filepath.Join("/dev/input", handler),
			Type:     devType,
			IsJetKVM: strings.Contains(name, "JetKVM"),
		})
	}

	return devices
}

// getDisplayInfo reads display information from DRM sysfs.
func getDisplayInfo() []DisplayInfo {
	var displays []DisplayInfo

	entries, err := filepath.Glob("/sys/class/drm/card*-*")
	if err != nil {
		return displays
	}

	for _, entry := range entries {
		connector := filepath.Base(entry)
		status := readSysFile(filepath.Join(entry, "status"))
		if status == "" {
			continue
		}

		info := DisplayInfo{
			Connector: connector,
			Status:    status,
		}

		if status == "connected" {
			modesData := readSysFile(filepath.Join(entry, "modes"))
			if modesData != "" {
				modes := strings.Split(modesData, "\n")
				info.Modes = modes
				if len(modes) > 0 {
					info.Resolution = modes[0] // First mode is the current/preferred
				}
			}
		}

		displays = append(displays, info)
	}

	return displays
}

// listMounts returns current mount points, filtered to interesting ones.
func listMounts() []MountInfo {
	var mounts []MountInfo

	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return mounts
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		device, mountPoint, fsType, options := fields[0], fields[1], fields[2], fields[3]

		// Filter: only show real devices and interesting mounts
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}
		// Skip internal partitions, show USB/media mounts
		if strings.HasPrefix(mountPoint, "/snap") {
			continue
		}

		mounts = append(mounts, MountInfo{
			Device:     device,
			MountPoint: mountPoint,
			FSType:     fsType,
			Options:    options,
		})
	}

	return mounts
}

func main() {
	port := "9182"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	agent := newAgent()

	// Start background device monitor (auto-discovers and reconnects)
	go agent.monitorAllDevices()

	// Log initial state
	devices := discoverJetKVMDevices()
	if len(devices) == 0 {
		log.Println("WARNING: No JetKVM input devices found initially. Will auto-discover when connected.")
	}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Get keyboard events (peek, doesn't clear)
	mux.HandleFunc("/events/keyboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			agent.keyboardEvents.Clear()
			json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
			return
		}
		events := agent.keyboardEvents.Get()
		json.NewEncoder(w).Encode(events)
	})

	// Get mouse events (peek, doesn't clear)
	mux.HandleFunc("/events/mouse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			agent.mouseEvents.Clear()
			json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
			return
		}
		events := agent.mouseEvents.Get()
		json.NewEncoder(w).Encode(events)
	})

	// Pop keyboard events (get + clear atomically)
	mux.HandleFunc("/events/keyboard/pop", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent.keyboardEvents.GetAndClear())
	})

	// Pop mouse events (get + clear atomically)
	mux.HandleFunc("/events/mouse/pop", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent.mouseEvents.GetAndClear())
	})

	// Clear all events
	mux.HandleFunc("/events/clear", func(w http.ResponseWriter, r *http.Request) {
		agent.keyboardEvents.Clear()
		agent.mouseEvents.Clear()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	})

	// List USB devices
	mux.HandleFunc("/usb/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listUSBDevices())
	})

	// List mounts
	mux.HandleFunc("/mounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listMounts())
	})

	// List input devices
	mux.HandleFunc("/input/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listInputDevices())
	})

	// Get display info
	mux.HandleFunc("/display", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getDisplayInfo())
	})

	log.Printf("JetKVM Remote Agent listening on :%s", port)
	log.Printf("Found %d JetKVM input device(s) initially (auto-reconnect enabled)", len(devices))
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
