package kvm

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/serialnet"
	"go.bug.st/serial"
)

// modeRecordingPort is a fake UART that records what is done to its line
// settings and break line.
type modeRecordingPort struct {
	*fakeSerialPort

	mu     sync.Mutex
	modes  []serial.Mode
	breaks int
}

func (p *modeRecordingPort) SetMode(m *serial.Mode) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modes = append(p.modes, *m)
	return nil
}

func (p *modeRecordingPort) Break(time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.breaks++
	return nil
}

func (p *modeRecordingPort) setModes() []serial.Mode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]serial.Mode(nil), p.modes...)
}

func (p *modeRecordingPort) breakCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.breaks
}

// useRecordingExtensionPort swaps in a UART that records line settings and
// breaks, points the saved settings at a temporary file, and puts every
// global the serial code touches back afterwards.
func useRecordingExtensionPort(t *testing.T) func() *modeRecordingPort {
	t.Helper()

	var mu sync.Mutex
	var ports []*modeRecordingPort
	originalPort := extPort
	extPort = newExtensionPort(func(*serial.Mode) (serial.Port, error) {
		p := &modeRecordingPort{fakeSerialPort: newFakeSerialPort()}
		mu.Lock()
		defer mu.Unlock()
		ports = append(ports, p)
		return p, nil
	})

	originalConfig, originalMode, originalPath := serialConfig, serialPortMode, serialSettingsPath
	originalDeviceConfig, originalDeviceConfigPath := config, configPath
	serialSettingsPath = filepath.Join(t.TempDir(), "serialSettings.json")
	config = &Config{}
	// Saving the device config fails, as on a machine without /userdata.
	configPath = filepath.Join(t.TempDir(), "no-such-dir", "kvm_config.json")

	t.Cleanup(func() {
		serialNetwork.Stop()
		_ = extPort.activate("")
		extPort = originalPort
		serialConfig, serialPortMode, serialSettingsPath = originalConfig, originalMode, originalPath
		config, configPath = originalDeviceConfig, originalDeviceConfigPath
	})

	return func() *modeRecordingPort {
		mu.Lock()
		defer mu.Unlock()
		return ports[len(ports)-1]
	}
}

func validSerialSettings() SerialSettings {
	s := serialConfig
	s.BaudRate, s.DataBits, s.Parity, s.StopBits = 9600, 8, "none", "1"
	s.normalizeNetwork()
	return s
}

func TestNetworkClientsCannotUsePortAfterAnotherExtensionTakesIt(t *testing.T) {
	current := useRecordingExtensionPort(t)

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	console := current()
	if err := extPort.setLineMode(&serial.Mode{BaudRate: 19200, DataBits: 8}); err != nil {
		t.Fatalf("the console's own line settings change failed: %v", err)
	}
	if err := extPort.sendBreak(time.Millisecond); err != nil || console.breakCount() != 1 {
		t.Fatalf("break = %v after %d breaks", err, console.breakCount())
	}

	// The ATX extension takes the UART; a network client that is still
	// connected must not reach it.
	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	atx := current()
	if err := extPort.setLineMode(&serial.Mode{BaudRate: 9600, DataBits: 8}); err != errNotSerialConsole {
		t.Fatalf("setLineMode on the ATX port returned %v", err)
	}
	if err := extPort.sendBreak(time.Millisecond); err != errNotSerialConsole {
		t.Fatalf("sendBreak on the ATX port returned %v", err)
	}
	if modes := atx.setModes(); len(modes) != 0 || atx.breakCount() != 0 {
		t.Fatalf("the ATX port was changed: modes %v, %d breaks", modes, atx.breakCount())
	}
}

func TestRejectedSerialSettingsAreNotSaved(t *testing.T) {
	useRecordingExtensionPort(t)

	for name, mutate := range map[string]func(*SerialSettings){
		"parity":    func(s *SerialSettings) { s.Parity = "sometimes" },
		"stop bits": func(s *SerialSettings) { s.StopBits = "3" },
		"port":      func(s *SerialSettings) { s.Ser2NetPort = 80 },
	} {
		s := validSerialSettings()
		s.NetworkMode = serialnet.ModeSer2Net
		mutate(&s)
		if err := setSerialSettings(s); err == nil {
			t.Errorf("%s: invalid settings were accepted", name)
		}
		if _, err := os.Stat(serialSettingsPath); !os.IsNotExist(err) {
			t.Errorf("%s: a rejected update left a settings file behind (%v)", name, err)
		}
	}

	// A valid update is saved, and survives a reload.
	s := validSerialSettings()
	s.NetworkMode = serialnet.ModeWeb
	if err := setSerialSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := getSerialSettings()
	if err != nil || got.NetworkMode != serialnet.ModeWeb || got.BaudRate != 9600 {
		t.Fatalf("reloaded %+v, %v", got, err)
	}
}

func TestReadingSerialSettingsKeepsAClientsLineMode(t *testing.T) {
	current := useRecordingExtensionPort(t)
	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	port := current()

	saved := validSerialSettings()
	if err := setSerialSettings(saved); err != nil {
		t.Fatal(err)
	}
	// An RFC 2217 client changes the baud rate for its session.
	clientMode := serial.Mode{BaudRate: 115200, DataBits: 8}
	if err := extPort.setLineMode(&clientMode); err != nil {
		t.Fatal(err)
	}
	before := len(port.setModes())

	// Opening the settings page reads them; that must not undo the client's.
	if _, err := getSerialSettings(); err != nil {
		t.Fatal(err)
	}
	modes := port.setModes()
	if len(modes) != before {
		t.Fatalf("reading the settings changed the line settings: %v", modes[before:])
	}
	if modes[len(modes)-1].BaudRate != 115200 {
		t.Fatalf("line settings are %+v, want the client's 115200", modes[len(modes)-1])
	}

	// So does saving a setting that has nothing to do with the line.
	echo := saved
	echo.EnableEcho = !saved.EnableEcho
	if err := setSerialSettings(echo); err != nil {
		t.Fatal(err)
	}
	if modes = port.setModes(); len(modes) != before {
		t.Fatalf("saving the echo option changed the line settings: %v", modes[before:])
	}

	// Changing the line settings themselves does apply them.
	faster := saved
	faster.BaudRate = 57600
	if err := setSerialSettings(faster); err != nil {
		t.Fatal(err)
	}
	if modes = port.setModes(); modes[len(modes)-1].BaudRate != 57600 {
		t.Fatalf("line settings are %+v after changing the baud rate", modes[len(modes)-1])
	}
	if err := setSerialSettings(saved); err != nil {
		t.Fatal(err)
	}

	// Mounting the console applies the saved ones, as at boot.
	if _, err := loadSerialSettings(true); err != nil {
		t.Fatal(err)
	}
	modes = port.setModes()
	if modes[len(modes)-1].BaudRate != 9600 {
		t.Fatalf("line settings are %+v after loading, want the saved 9600", modes[len(modes)-1])
	}
}

func TestExtensionSwitchStartsAndStopsNetworkAccessWhetherOrNotConfigSaves(t *testing.T) {
	useRecordingExtensionPort(t)

	// Every config save fails here; the network side must not depend on it.
	web := validSerialSettings()
	web.NetworkMode = serialnet.ModeWeb
	if err := setSerialSettings(web); err != nil {
		t.Fatal(err)
	}

	_ = rpcSetActiveExtension("serial-console")
	if !extPort.active("serial-console") || !serialNetwork.Status().Running {
		t.Fatalf("the console is not serving after the switch: %+v", serialNetwork.Status())
	}

	_ = rpcSetActiveExtension("atx-power")
	if !extPort.active("atx-power") {
		t.Fatal("the ATX extension is not active")
	}
	if st := serialNetwork.Status(); st.Running || len(st.Clients) != 0 {
		t.Fatalf("network access outlived the console: %+v", st)
	}
}
