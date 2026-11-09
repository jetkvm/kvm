package kvm

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jetkvm/kvm/internal/osservices"
)

// OSServicesConfig is the OS Services part of the KVM settings, in the same
// JSON shape as the JetKVM Mini's, which the browser reads and writes with
// getOsServicesSettings and setOsServicesSettings.
type OSServicesConfig struct {
	TerminalEnabled  bool   `json:"terminal_enabled"`
	FilesEnabled     bool   `json:"files_enabled"`
	CaptureDisplay   string `json:"capture_display"`
	VideoEnabled     bool   `json:"video_enabled"`
	Resolution       string `json:"resolution"`
	DisplayMode      string `json:"display_mode"`
	Adaptation       string `json:"adaptation,omitempty"`
	ClipboardEnabled bool   `json:"clipboard_enabled"`
}

type osServicesResolution struct {
	name          string
	width, height uint32
}

var osServicesResolutions = []osServicesResolution{
	{"1280x720", 1280, 720},
	{"1920x1080", 1920, 1080},
	{"2560x1440", 2560, 1440},
	{"3840x2160", 3840, 2160},
}

const (
	osServicesDefaultResolution = "2560x1440"
	// osServicesAdaptationMax bounds the adaptation text.
	osServicesAdaptationMax = 39
)

func defaultOSServicesConfig() *OSServicesConfig {
	return &OSServicesConfig{
		TerminalEnabled:  true,
		FilesEnabled:     true,
		VideoEnabled:     true,
		ClipboardEnabled: true,
		Resolution:       osServicesDefaultResolution,
		DisplayMode:      "extend",
	}
}

// parseOSServicesAdaptation reads "", "off", "auto" (no fixed rate) or
// "fixed:<kbps>:<fps>".
func parseOSServicesAdaptation(text string) (kbps, fps uint32, ok bool) {
	if text == "" || text == "off" || text == "auto" {
		return 0, 0, true
	}
	rest, found := strings.CutPrefix(text, "fixed:")
	if !found {
		return 0, 0, false
	}
	k, f, found := strings.Cut(rest, ":")
	if !found {
		return 0, 0, false
	}
	parse := func(s string) (uint32, bool) {
		if s == "" || len(s) > 10 || strings.TrimLeft(s, "0123456789") != "" {
			return 0, false
		}
		v, err := strconv.ParseUint(s, 10, 32)
		return uint32(v), err == nil
	}
	kv, ok1 := parse(k)
	fv, ok2 := parse(f)
	return kv, fv, ok1 && ok2
}

func validCaptureDisplay(text string) bool {
	return len(text) <= osservices.CaptureDisplayMax && !strings.ContainsAny(text, "\x00\r\n")
}

// mergeOSServicesConfig applies a setOsServicesSettings request: a member
// that is left out or null keeps its value, and any invalid member rejects
// the whole request.
func mergeOSServicesConfig(base OSServicesConfig, request map[string]any) (OSServicesConfig, error) {
	merged := base
	field := func(name string) (any, bool) {
		value, ok := request[name]
		return value, ok && value != nil
	}
	bools := []struct {
		name  string
		value *bool
	}{
		{"terminal_enabled", &merged.TerminalEnabled},
		{"files_enabled", &merged.FilesEnabled},
		{"video_enabled", &merged.VideoEnabled},
		{"clipboard_enabled", &merged.ClipboardEnabled},
	}
	for _, b := range bools {
		if value, ok := field(b.name); ok {
			v, isBool := value.(bool)
			if !isBool {
				return base, errors.New(b.name + " must be a boolean")
			}
			*b.value = v
		}
	}
	if value, ok := field("capture_display"); ok {
		v, isString := value.(string)
		if !isString || !validCaptureDisplay(v) {
			return base, errors.New("invalid capture display name")
		}
		merged.CaptureDisplay = v
	}
	if value, ok := field("resolution"); ok {
		v, _ := value.(string)
		if _, found := osServicesResolutionSize(v); !found {
			return base, errors.New("unsupported OS Services resolution")
		}
		merged.Resolution = v
	}
	if value, ok := field("display_mode"); ok {
		v, _ := value.(string)
		if v != "mirror" && v != "extend" {
			return base, errors.New("unsupported OS Services display mode")
		}
		merged.DisplayMode = v
	}
	if value, ok := field("adaptation"); ok {
		v, isString := value.(string)
		if !isString || len(v) > osServicesAdaptationMax {
			return base, errors.New("invalid adaptation value")
		}
		if _, _, valid := parseOSServicesAdaptation(v); !valid {
			return base, errors.New("invalid adaptation value")
		}
		merged.Adaptation = v
	}
	return merged, nil
}

func osServicesResolutionSize(name string) (osServicesResolution, bool) {
	for _, r := range osServicesResolutions {
		if r.name == name {
			return r, true
		}
	}
	return osServicesResolution{}, false
}

// sessionSettings maps the KVM settings onto what the session acts on.
func (c OSServicesConfig) sessionSettings(codec osservices.Codec) osservices.Settings {
	r, ok := osServicesResolutionSize(c.Resolution)
	if !ok {
		r, _ = osServicesResolutionSize(osServicesDefaultResolution)
	}
	s := osservices.Settings{
		ClipboardEnabled: c.ClipboardEnabled,
		TerminalEnabled:  c.TerminalEnabled,
		FilesEnabled:     c.FilesEnabled,
		VideoEnabled:     c.VideoEnabled,
		CaptureDisplay:   c.CaptureDisplay,
		Width:            r.width,
		Height:           r.height,
		Layout:           osservices.LayoutExtend,
		Codec:            codec,
	}
	if c.DisplayMode == "mirror" {
		s.Layout = osservices.LayoutMirror
	}
	if kbps, fps, ok := parseOSServicesAdaptation(c.Adaptation); ok {
		s.TargetKbps = kbps
		s.MaximumFPS = fps
	}
	return s
}
