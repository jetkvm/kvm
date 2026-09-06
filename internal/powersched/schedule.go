// Package powersched describes recurring power actions for the attached host.
//
// It holds only the schedule model and its translation to a crontab, with no
// dependency on the device runtime, so the rules can be unit tested on any
// platform. Executing a schedule lives in the main kvm package.
package powersched

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

// Schedule methods.
const (
	MethodWOL = "wol"
	MethodATX = "atx"
)

// Schedule actions.
const (
	ActionOn       = "on"
	ActionOff      = "off"
	ActionOffForce = "off-force"
)

// MaxSchedules limits how many schedules a device may store, mirroring the
// keyboard macro limits so a misbehaving client can't grow the config forever.
const MaxSchedules = 25

// Schedule describes a recurring power action on the attached host.
//
// The schedule is stored as a weekday set plus a wall-clock time in an IANA
// timezone rather than a raw crontab: the UI exposes a weekday/time picker, and
// keeping the structured form lets both ends render the schedule consistently.
type Schedule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Method   string `json:"method"`   // "wol" | "atx"
	Action   string `json:"action"`   // "on" | "off" | "off-force"
	Weekdays []int  `json:"weekdays"` // 0=Sunday .. 6=Saturday
	Hour     int    `json:"hour"`     // 0-23
	Minute   int    `json:"minute"`   // 0-59
	Timezone string `json:"timezone"` // IANA name, e.g. "Europe/Berlin"

	// Wake-on-LAN only. The MAC is copied onto the schedule rather than
	// referencing an entry in WakeOnLanDevices, so removing a saved device
	// can't leave a schedule pointing at nothing.
	MacAddress  string `json:"macAddress,omitempty"`
	BroadcastIP string `json:"broadcastIP,omitempty"`
}

// AllowedActions returns the actions that are valid for a given method.
func AllowedActions(method string) []string {
	switch method {
	case MethodWOL:
		// A magic packet can only ever turn a host on.
		return []string{ActionOn}
	case MethodATX:
		return []string{ActionOn, ActionOff, ActionOffForce}
	default:
		return nil
	}
}

// Validate checks the schedule and normalises its weekday list. It returns an
// error describing the first problem found.
func (s *Schedule) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("schedule name cannot be empty")
	}

	actions := AllowedActions(s.Method)
	if actions == nil {
		return fmt.Errorf("invalid method: %s", s.Method)
	}

	valid := false
	for _, a := range actions {
		if s.Action == a {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("action %q is not valid for method %q", s.Action, s.Method)
	}

	if s.Method == MethodWOL {
		if _, err := net.ParseMAC(s.MacAddress); err != nil {
			return fmt.Errorf("invalid MAC address %q: %w", s.MacAddress, err)
		}
		if s.BroadcastIP != "" {
			if ip := net.ParseIP(s.BroadcastIP); ip == nil || ip.To4() == nil {
				return fmt.Errorf("invalid broadcast IP address: %s", s.BroadcastIP)
			}
		}
	}

	if s.Hour < 0 || s.Hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23, got %d", s.Hour)
	}
	if s.Minute < 0 || s.Minute > 59 {
		return fmt.Errorf("minute must be between 0 and 59, got %d", s.Minute)
	}

	if len(s.Weekdays) == 0 {
		return fmt.Errorf("at least one weekday must be selected")
	}
	seen := make(map[int]bool, len(s.Weekdays))
	days := make([]int, 0, len(s.Weekdays))
	for _, d := range s.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("weekday must be between 0 and 6, got %d", d)
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		days = append(days, d)
	}
	sort.Ints(days)
	s.Weekdays = days

	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", s.Timezone, err)
		}
	}

	return nil
}

// CronTab renders the schedule as a 6-field crontab, matching the
// with-seconds format the jiggler already uses.
func (s *Schedule) CronTab() string {
	days := make([]string, 0, len(s.Weekdays))
	for _, d := range s.Weekdays {
		days = append(days, fmt.Sprintf("%d", d))
	}

	tab := fmt.Sprintf("0 %d %d * * %s", s.Minute, s.Hour, strings.Join(days, ","))
	if s.Timezone != "" && s.Timezone != "UTC" {
		tab = fmt.Sprintf("TZ=%s %s", s.Timezone, tab)
	}
	return tab
}
