package release

import (
	"encoding/json"
	"testing"
)

// The JSON the browser reads, member for member as the Mini firmware
// renders it.
func TestRender(t *testing.T) {
	newest := &Release{Version: "0.4.2", Packages: []Package{{Name: "win", Size: 10}}}
	older := Sender{Version: "0.4.1", Package: "win"}
	for _, tc := range []struct {
		name      string
		newest    *Release
		sender    Sender
		progress  Progress
		automatic bool
		want      string
	}{
		{"nothing known", nil, older, Progress{}, false, `null`},
		{"offer", newest, older, Progress{}, false,
			`{"version":"0.4.2","available":true,"installable":true,"automatic":false}`},
		{"no sender", newest, Sender{}, Progress{}, true,
			`{"version":"0.4.2","available":false,"installable":false,"automatic":true}`},
		{"sender without the package", newest, Sender{Version: "0.4.1", Package: "mac"}, Progress{}, false,
			`{"version":"0.4.2","available":true,"installable":false,"automatic":false}`},
		{"downloading", newest, older, Progress{Phase: PhaseDownloading, Version: "0.4.2", Size: 10}, false,
			`{"version":"0.4.2","available":true,"installable":true,"automatic":false,"phase":"downloading","sent":0,"size":10}`},
		{"failed", newest, older, Progress{Phase: PhaseFailed, Version: "0.4.2", Sent: 4, Size: 10, Error: "the update ended"}, false,
			`{"version":"0.4.2","available":true,"installable":true,"automatic":false,"phase":"failed","sent":4,"size":10,"error":"the update ended"}`},
		{"done, sender current", newest, Sender{Version: "0.4.2", Package: "win"}, Progress{Phase: PhaseDone, Version: "0.4.2", Sent: 10, Size: 10}, false,
			`{"version":"0.4.2","available":false,"installable":true,"automatic":false,"phase":"done","sent":10,"size":10}`},
		// A finished update gives way to a newer release.
		{"done, newer release", &Release{Version: "0.4.3", Packages: []Package{{Name: "win"}}}, Sender{Version: "0.4.2", Package: "win"},
			Progress{Phase: PhaseDone, Version: "0.4.2", Sent: 10, Size: 10}, false,
			`{"version":"0.4.3","available":true,"installable":true,"automatic":false}`},
		// A running one does not.
		{"installing, newer release", &Release{Version: "0.4.3", Packages: []Package{{Name: "win"}}}, older,
			Progress{Phase: PhaseInstalling, Version: "0.4.2", Sent: 10, Size: 10}, false,
			`{"version":"0.4.2","available":true,"installable":true,"automatic":false,"phase":"installing","sent":10,"size":10}`},
		// The last update stays while no release is known.
		{"failed, no release", nil, older, Progress{Phase: PhaseFailed, Version: "0.4.2", Error: "x"}, false,
			`{"version":"0.4.2","available":false,"installable":false,"automatic":false,"phase":"failed","sent":0,"size":0,"error":"x"}`},
	} {
		got, err := json.Marshal(Render(tc.newest, tc.sender, tc.progress, tc.automatic))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
