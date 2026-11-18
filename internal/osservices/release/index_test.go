package release

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const sha = "77542dc59b44ff5a67e2bf76eca126fdfed1bcfc6d478e9bdba6e6d89eb3fb6b"

func readTestIndex(t *testing.T, name string, signed bool) Release {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	release, err := ReadIndex(data, signed)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return release
}

// The bucket's indexes as of 2026-11-09: releases 0.4.0 to 0.4.5, and dev
// builds up to 0.4.5-dev202610210854.
func TestReadIndexFromBucket(t *testing.T) {
	stable := readTestIndex(t, "releases.json", true)
	if stable.Version != "0.4.5" || !stable.Signed || len(stable.Packages) != 3 {
		t.Fatalf("stable = %s signed=%v with %d packages", stable.Version, stable.Signed, len(stable.Packages))
	}
	linux := stable.Package("linux-x86_64-appimage")
	if linux == nil || linux.Path != "0.4.5/IP-KVM-OS-Services_0.4.5_linux-x86_64.AppImage" ||
		linux.Size != 88685048 || hex.EncodeToString(linux.SHA256[:]) != sha {
		t.Fatalf("linux package = %+v", linux)
	}

	dev := readTestIndex(t, "releases-dev.json", false)
	if dev.Version != "0.4.5-dev202610210854" || dev.Signed {
		t.Fatalf("dev = %s signed=%v", dev.Version, dev.Signed)
	}
	if mac := dev.Package("macos-aarch64"); mac == nil || mac.Size != 1855071 {
		t.Fatalf("dev mac package = %+v", mac)
	}

	// The release outranks its own dev builds.
	if got := Newest(&stable, &dev, true); got != &stable {
		t.Fatalf("newest with dev = %s", got.Version)
	}
	// Each index holds only its own channel.
	if got := readTestIndex(t, "releases-dev.json", true); got.Version != "" {
		t.Fatalf("dev index read as signed gave %s", got.Version)
	}
	if got := readTestIndex(t, "releases.json", false); got.Version != "" {
		t.Fatalf("stable index read as dev gave %s", got.Version)
	}
}

func pkgJSON(path string, size any, digest string) string {
	sizeJSON, _ := json.Marshal(size)
	return fmt.Sprintf(`{"path":%q,"size":%s,"sha256":%q}`, path, sizeJSON, digest)
}

func TestReadIndexRange(t *testing.T) {
	index := `{"other":[1,{"x":"]"}],"releases":[
		{"version":"0.5.0","packages":{"a":` + pkgJSON("0.5.0/a", 1, sha) + `}},
		{"version":"0.3.9","packages":{"a":` + pkgJSON("0.3.9/a", 1, sha) + `}},
		{"version":"0.4.9-dev1","packages":{"a":` + pkgJSON("dev/a", 1, sha) + `}},
		{"version":"v0.4.1","packages":{"a":` + pkgJSON("0.4.1/a", 1, sha) + `}},
		{"version":"0.4.0","packages":{"a":` + pkgJSON("0.4.0/a", 1, sha) + `}},
		{"version":7}, "text", null, {"version":"not a version"}
	]}`
	got, err := ReadIndex([]byte(index), true)
	if err != nil {
		t.Fatal(err)
	}
	// The newest 0.4.x release, without its "v"; its packages, not those of
	// a release it replaced.
	if got.Version != "0.4.1" || len(got.Packages) != 1 || got.Packages[0].Path != "0.4.1/a" {
		t.Fatalf("got %+v", got)
	}

	got, err = ReadIndex([]byte(index), false)
	if err != nil || got.Version != "0.4.9-dev1" {
		t.Fatalf("dev: %+v, %v", got, err)
	}
}

func TestReadIndexSkipsBadPackages(t *testing.T) {
	bad := map[string]string{
		"parent":                pkgJSON("0.4.0/../x", 1, sha),
		"absolute":              pkgJSON("/0.4.0/x", 1, sha),
		"query":                 pkgJSON("0.4.0/x?y", 1, sha),
		"long-path":             pkgJSON(strings.Repeat("a", PathMax+1), 1, sha),
		"zero":                  pkgJSON("0.4.0/x", 0, sha),
		"too-big":               pkgJSON("0.4.0/x", PackageMaxBytes+1, sha),
		"fraction":              pkgJSON("0.4.0/x", 1.5, sha),
		"size-string":           pkgJSON("0.4.0/x", "1", sha),
		"short-sha":             pkgJSON("0.4.0/x", 1, sha[:62]),
		"bad-sha":               pkgJSON("0.4.0/x", 1, "zz"+sha[2:]),
		"no-path":               `{"size":1,"sha256":"` + sha + `"}`,
		"not-object":            `"x"`,
		strings.Repeat("n", 65): pkgJSON("0.4.0/x", 1, sha),
	}
	var members []string
	for name, value := range bad {
		members = append(members, fmt.Sprintf("%q:%s", name, value))
	}
	// Ten good ones, in index order; the first eight are kept.
	for i := range 10 {
		members = append(members, fmt.Sprintf(`"good-%d":%s`, i, pkgJSON(fmt.Sprintf("0.4.0/g%d", i), 4096.0, strings.ToUpper(sha))))
	}
	index := `{"releases":[{"version":"0.4.0","packages":{` + strings.Join(members, ",") + `}}]}`
	got, err := ReadIndex([]byte(index), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != PackagesMax {
		t.Fatalf("%d packages: %+v", len(got.Packages), got.Packages)
	}
	for i, pkg := range got.Packages {
		if pkg.Name != fmt.Sprintf("good-%d", i) || pkg.Size != 4096 || hex.EncodeToString(pkg.SHA256[:]) != sha {
			t.Fatalf("package %d = %+v", i, pkg)
		}
	}
}

func TestReadIndexErrors(t *testing.T) {
	for _, tc := range []struct{ index, want string }{
		{`{"version":"0.4.0"}`, "the index has no releases list"},
		{`{"releases":{}}`, "the index has no releases list"},
		{`[]`, "the index has no releases list"},
		{`<html>`, "the index has no releases list"},
		{`{"releases":[{"version":"0.4.0"},`, "the index is truncated"},
		{`{"releases":[{"version":"0.4.0"}`, "the index is truncated"},
		{`{"releases":[{"version":"0.4.0"},]}`, "the index has a malformed release"},
		{`{"releases":[{"version" "0.4.0"}]}`, "the index has a malformed release"},
	} {
		if _, err := ReadIndex([]byte(tc.index), true); err == nil || err.Error() != tc.want {
			t.Errorf("%s: %v, want %q", tc.index, err, tc.want)
		}
	}
	got, err := ReadIndex([]byte(`{"releases":[]}`), true)
	if err != nil || got.Version != "" {
		t.Fatalf("empty list: %+v, %v", got, err)
	}
}

func TestNewestAndTarget(t *testing.T) {
	stable := Release{Version: "0.4.2", Signed: true, Packages: []Package{{Name: "win", Size: 1}}}
	dev := Release{Version: "0.4.3-dev1", Packages: []Package{{Name: "win", Size: 2}}}
	none := Release{}

	if got := Newest(&stable, &dev, false); got != &stable {
		t.Fatal("dev release chosen without the dev channel")
	}
	if got := Newest(&stable, &dev, true); got != &dev {
		t.Fatal("newer dev release not chosen")
	}
	if got := Newest(&none, &dev, false); got != nil {
		t.Fatal("a release without either index")
	}
	if got := Newest(&none, &none, true); got != nil {
		t.Fatal("a release from empty indexes")
	}

	for _, tc := range []struct {
		sender Sender
		want   bool
	}{
		{Sender{Version: "0.4.1", Package: "win"}, true},
		{Sender{Version: "0.4.2-dev9", Package: "win"}, true},
		{Sender{Version: "0.4.2", Package: "win"}, false},
		{Sender{Version: "0.4.3", Package: "win"}, false},
		{Sender{Version: "0.4.1", Package: "mac"}, false},
		{Sender{Version: "0.4.1"}, false},
		{Sender{Package: "win"}, false},
	} {
		if got := Target(&stable, tc.sender) != nil; got != tc.want {
			t.Errorf("%+v: target %v, want %v", tc.sender, got, tc.want)
		}
	}
	if Target(nil, Sender{Version: "0.4.1", Package: "win"}) != nil {
		t.Fatal("a target without a release")
	}
}
