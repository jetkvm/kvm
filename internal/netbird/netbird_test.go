package netbird

import (
	"errors"
	"testing"
)

var errDaemonUnreachable = errors.New("daemon unreachable")

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantErr        bool
		wantRunning    bool
		wantDaemon     string
		wantManagement string
		wantFQDN       string
		wantIP         string
		wantPeersTotal int
	}{
		{
			name: "connected",
			input: `{
				"daemonStatus": "Connected",
				"management": {"url": "https://mesh.example.com:443", "connected": true, "error": ""},
				"netbirdIp": "100.0.1.2/16",
				"fqdn": "device.mesh.example.com",
				"peers": {"total": 4, "connected": 3}
			}`,
			wantRunning:    true,
			wantDaemon:     "Connected",
			wantManagement: "https://mesh.example.com:443",
			wantFQDN:       "device.mesh.example.com",
			wantIP:         "100.0.1.2/16",
			wantPeersTotal: 4,
		},
		{
			name: "needs login",
			input: `{
				"daemonStatus": "NeedsLogin",
				"management": {"url": "", "connected": false, "error": ""}
			}`,
			wantRunning:    false,
			wantDaemon:     "NeedsLogin",
			wantManagement: DefaultManagementURL,
		},
		{
			name: "management error",
			input: `{
				"daemonStatus": "Connecting",
				"management": {"url": "https://mesh.example.com:443", "connected": false, "error": "certificate signed by unknown authority"}
			}`,
			wantRunning:    false,
			wantDaemon:     "Connecting",
			wantManagement: "https://mesh.example.com:443",
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := ParseStatus([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.Running != tt.wantRunning {
				t.Errorf("Running = %v, want %v", status.Running, tt.wantRunning)
			}
			if status.DaemonStatus != tt.wantDaemon {
				t.Errorf("DaemonStatus = %q, want %q", status.DaemonStatus, tt.wantDaemon)
			}
			if status.ManagementURL != tt.wantManagement {
				t.Errorf("ManagementURL = %q, want %q", status.ManagementURL, tt.wantManagement)
			}
			if tt.wantFQDN != "" && status.FQDN != tt.wantFQDN {
				t.Errorf("FQDN = %q, want %q", status.FQDN, tt.wantFQDN)
			}
			if tt.wantIP != "" && status.IP != tt.wantIP {
				t.Errorf("IP = %q, want %q", status.IP, tt.wantIP)
			}
			if tt.wantPeersTotal != 0 && status.PeersTotal != tt.wantPeersTotal {
				t.Errorf("PeersTotal = %d, want %d", status.PeersTotal, tt.wantPeersTotal)
			}
		})
	}
}

func TestNormalizeManagementURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "trims trailing slash", input: "https://mesh.example.com/", want: "https://mesh.example.com"},
		{name: "trims whitespace", input: "  https://mesh.example.com  ", want: "https://mesh.example.com"},
		{name: "missing scheme", input: "mesh.example.com", wantErr: true},
		{name: "bad scheme", input: "ftp://mesh.example.com", wantErr: true},
		{name: "with query", input: "https://mesh.example.com?x=1", wantErr: true},
		{name: "with user info", input: "https://user@mesh.example.com", wantErr: true},
		{name: "with path", input: "https://mesh.example.com/foo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeManagementURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveManagementURL(t *testing.T) {
	if got := EffectiveManagementURL(""); got != DefaultManagementURL {
		t.Errorf("got %q, want %q", got, DefaultManagementURL)
	}
	if got := EffectiveManagementURL("https://mesh.example.com"); got != "https://mesh.example.com" {
		t.Errorf("got %q, want %q", got, "https://mesh.example.com")
	}
}

func TestGetStatusNotInstalled(t *testing.T) {
	origCheck := CheckInstalled
	defer func() { CheckInstalled = origCheck }()
	CheckInstalled = func() bool { return false }

	status, err := GetStatus(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Installed {
		t.Errorf("Installed = true, want false")
	}
	if status.ManagementURL != DefaultManagementURL {
		t.Errorf("ManagementURL = %q, want %q", status.ManagementURL, DefaultManagementURL)
	}
}

func TestGetStatusExecError(t *testing.T) {
	origCheck := CheckInstalled
	origExec := ExecCommand
	defer func() {
		CheckInstalled = origCheck
		ExecCommand = origExec
	}()
	CheckInstalled = func() bool { return true }
	warned := false
	ExecCommand = func(args ...string) ([]byte, error) {
		return nil, errDaemonUnreachable
	}

	status, err := GetStatus(func(error) { warned = true })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Installed {
		t.Errorf("Installed = false, want true")
	}
	if status.Running {
		t.Errorf("Running = true, want false")
	}
	if !warned {
		t.Errorf("warn callback was not invoked")
	}
}
