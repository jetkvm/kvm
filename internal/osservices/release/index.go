// Package release is the OS Services update relay's pure part: the release
// indexes, the choice of the package for a sender, the browser's guestUpdate
// object, and the checks that keep an unchecked package from completing on
// the sender.
//
// The release bucket holds releases.json, signed with the JetKVM release key
// (releases.json.sig), and releases-dev.json, unsigned, for the dev channel.
// Each lists releases with a package per sender platform:
//
//	{"releases":[{"version":"0.4.2","packages":{"linux-x86_64-appimage":
//	  {"path":"0.4.2/...AppImage","size":123,"sha256":"<64 hex>"}}}]}
//
// This firmware installs 0.4.x: releases from the signed index and
// prereleases from the dev index. Only the newest release of each index is
// kept.
package release

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/osservices"
)

const (
	// The range of releases this firmware installs: 0.4.x. A new minor
	// version can change what the sender needs from the KVM.
	Major = 0
	Minor = 4
	// PackagesMax is the most packages kept per release, in index order.
	PackagesMax = 8
	// PathMax is the longest package path, relative to the index.
	PathMax = 128
	// PackageMaxBytes is the largest package the KVM relays.
	PackageMaxBytes = 512 << 20
	// IndexMax and SignatureMax are the largest index and .sig files read.
	IndexMax     = 256 << 10
	SignatureMax = 4096
)

// Package is one sender platform's file in a release.
type Package struct {
	Name   string
	Path   string // relative to the index
	Size   uint64
	SHA256 [32]byte
}

// Release is the newest release in range of one index.
type Release struct {
	// Version is empty when the index lists no release in range.
	Version string
	// Signed: from the signed index, so its packages carry signatures.
	Signed   bool
	Packages []Package
}

var (
	errNoReleases       = errors.New("the index has no releases list")
	errMalformedRelease = errors.New("the index has a malformed release")
	errTruncated        = errors.New("the index is truncated")
)

// ReadIndex reads an index and returns its newest release in range (Version
// empty when there is none). A release or a package that is incomplete or
// malformed is skipped; an index that is not JSON, or has no releases list,
// is an error.
func ReadIndex(data []byte, signed bool) (Release, error) {
	var newest Release
	dec := json.NewDecoder(bytes.NewReader(data))
	if !openToken(dec, '{') {
		return newest, errNoReleases
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return newest, indexError(err, data)
		}
		if key != "releases" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return newest, indexError(err, data)
			}
			continue
		}
		if !openToken(dec, '[') {
			return newest, errNoReleases
		}
		for dec.More() {
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return newest, indexError(err, data)
			}
			readRelease(raw, signed, &newest)
		}
		if _, err := dec.Token(); err != nil {
			return newest, indexError(err, data)
		}
		return newest, nil
	}
	return newest, errNoReleases
}

// openToken reads the next token and reports whether it is delim.
func openToken(dec *json.Decoder, delim json.Delim) bool {
	token, err := dec.Token()
	return err == nil && token == delim
}

// indexError: input that ends inside the releases list is truncated, any
// other error is a malformed release.
func indexError(err error, data []byte) error {
	var syntax *json.SyntaxError
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		(errors.As(err, &syntax) && syntax.Offset >= int64(len(bytes.TrimRight(data, " \t\r\n")))) {
		return errTruncated
	}
	return errMalformedRelease
}

// readRelease replaces *newest with the release in raw when it is usable
// and newer.
func readRelease(raw json.RawMessage, signed bool, newest *Release) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	version, ok := usableVersion(fields["version"], signed)
	if !ok || (newest.Version != "" && !Newer(version, newest.Version)) {
		return
	}
	*newest = Release{Version: version, Signed: signed}
	readPackages(fields["packages"], newest)
}

// usableVersion is the version of a release in range for its index,
// without a leading "v": a release in the signed index, a prerelease in the
// dev index.
func usableVersion(raw json.RawMessage, signed bool) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil || len(text) > osservices.UpdateVersionMax {
		return "", false
	}
	v, ok := parseVersion(text)
	if !ok || v.Major() != Major || v.Minor() != Minor || (v.Prerelease() == "") != signed {
		return "", false
	}
	return stripV(text), true
}

// readPackages adds the valid packages of an object, in index order.
func readPackages(raw json.RawMessage, release *Release) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if !openToken(dec, '{') {
		return
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return
		}
		var item json.RawMessage
		if dec.Decode(&item) != nil {
			return
		}
		name, _ := key.(string)
		if pkg, ok := readPackage(name, item); ok && len(release.Packages) < PackagesMax {
			release.Packages = append(release.Packages, pkg)
		}
	}
}

func readPackage(name string, raw json.RawMessage) (Package, bool) {
	var fields map[string]json.RawMessage
	if len(name) > osservices.UpdatePackageMax || json.Unmarshal(raw, &fields) != nil {
		return Package{}, false
	}
	pkg := Package{Name: name}
	var size float64
	var digest string
	if json.Unmarshal(fields["path"], &pkg.Path) != nil || !pathValid(pkg.Path) ||
		json.Unmarshal(fields["size"], &size) != nil ||
		size < 1 || size > PackageMaxBytes || size != math.Trunc(size) ||
		json.Unmarshal(fields["sha256"], &digest) != nil || len(digest) != 64 {
		return Package{}, false
	}
	pkg.Size = uint64(size)
	if _, err := hex.Decode(pkg.SHA256[:], []byte(digest)); err != nil {
		return Package{}, false
	}
	return pkg, true
}

// pathValid: a relative path in the bucket, with no scheme, query or parent
// steps.
func pathValid(path string) bool {
	if path == "" || path[0] == '/' || len(path) > PathMax || strings.Contains(path, "..") {
		return false
	}
	for _, c := range path {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("._-+~/", c):
		default:
			return false
		}
	}
	return true
}

func stripV(text string) string {
	if text != "" && (text[0] == 'v' || text[0] == 'V') {
		return text[1:]
	}
	return text
}

func parseVersion(text string) (*semver.Version, bool) {
	v, err := semver.NewVersion(stripV(text))
	return v, err == nil
}

// Newer reports whether version is newer than than; false when either does
// not parse.
func Newer(version, than string) bool {
	a, okA := parseVersion(version)
	b, okB := parseVersion(than)
	return okA && okB && a.GreaterThan(b)
}

// Newest is the newer of the two releases, the dev release only when
// includeDev; nil when neither has one.
func Newest(stable, dev *Release, includeDev bool) *Release {
	var newest *Release
	if stable.Version != "" {
		newest = stable
	}
	if includeDev && dev.Version != "" && (newest == nil || Newer(dev.Version, newest.Version)) {
		newest = dev
	}
	return newest
}

// Package is the release's package for a sender platform, or nil.
func (r *Release) Package(name string) *Package {
	if r == nil || name == "" {
		return nil
	}
	for i := range r.Packages {
		if r.Packages[i].Name == name {
			return &r.Packages[i]
		}
	}
	return nil
}

// Sender is the connected sender; Version is empty when none has reported
// one, Package when it cannot install packages.
type Sender struct {
	Version string
	Package string
}

// Target is the package to install on the sender: newest is newer than the
// sender and lists its package. nil otherwise.
func Target(newest *Release, sender Sender) *Package {
	if newest == nil || !Newer(newest.Version, sender.Version) {
		return nil
	}
	return newest.Package(sender.Package)
}
