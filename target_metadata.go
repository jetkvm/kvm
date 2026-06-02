package kvm

import (
	"sync"
	"time"
)

const companionTargetTTL = 2 * time.Minute

type TargetMetadata struct {
	TargetType            string       `json:"target_type"`
	PreferredMouseMode    string       `json:"preferred_mouse_mode,omitempty"`
	DisplayWidth          int          `json:"display_width,omitempty"`
	DisplayHeight         int          `json:"display_height,omitempty"`
	DisplayAspect         float64      `json:"display_aspect,omitempty"`
	Evidence              []string     `json:"evidence,omitempty"`
	Source                string       `json:"source,omitempty"`
	LastSeenUnixMilli     int64        `json:"last_seen_unix_milli,omitempty"`
	LeaseExpiresUnixMilli int64        `json:"lease_expires_unix_milli,omitempty"`
	HDMIReconnectRequired bool         `json:"hdmi_reconnect_required,omitempty"`
	FallbackDisplayMode   *DisplayMode `json:"fallback_display_mode,omitempty"`
	CompanionNotice       string       `json:"companion_notice,omitempty"`
	Fresh                 bool         `json:"fresh"`
}

type CompanionTargetDeclaration struct {
	State                            string   `json:"state"`
	JetKVMUSBIdentity                string   `json:"jetkvm_usb_identity"`
	TargetType                       string   `json:"target_type"`
	PreferredMouseMode               string   `json:"preferred_mouse_mode"`
	DisplayWidth                     int      `json:"display_width"`
	DisplayHeight                    int      `json:"display_height"`
	DisplayAspect                    float64  `json:"display_aspect"`
	Evidence                         []string `json:"evidence"`
	LeaseMs                          int64    `json:"lease_ms"`
	NotificationPermissionGranted    bool     `json:"notification_permission_granted"`
	DisplayOverAppsPermissionGranted bool     `json:"display_over_apps_permission_granted"`
	BatteryUnrestrictedGranted       bool     `json:"battery_unrestricted_granted"`
	PairedJetKVMURLs                 []string `json:"paired_jetkvm_urls"`
	VisibleIPs                       []string `json:"visible_ips"`
}

var (
	targetMetadataLock sync.Mutex
	companionTarget    TargetMetadata
)

func setCompanionTargetMetadata(declaration CompanionTargetDeclaration) TargetMetadata {
	now := time.Now()
	if declaration.State == "disconnected" {
		targetMetadataLock.Lock()
		defer targetMetadataLock.Unlock()

		companionTarget = TargetMetadata{
			TargetType:        "android",
			Source:            "companion",
			LastSeenUnixMilli: now.UnixMilli(),
			Fresh:             false,
		}
		return companionTarget
	}

	aspect := declaration.DisplayAspect
	if aspect <= 0 && declaration.DisplayWidth > 0 && declaration.DisplayHeight > 0 {
		aspect = float64(declaration.DisplayWidth) / float64(declaration.DisplayHeight)
	}
	lease := time.Duration(declaration.LeaseMs) * time.Millisecond
	if lease <= 0 {
		lease = companionTargetTTL
	}
	if lease > companionTargetTTL {
		lease = companionTargetTTL
	}

	metadata := TargetMetadata{
		TargetType:            declaration.TargetType,
		PreferredMouseMode:    declaration.PreferredMouseMode,
		DisplayWidth:          declaration.DisplayWidth,
		DisplayHeight:         declaration.DisplayHeight,
		DisplayAspect:         aspect,
		Evidence:              append([]string(nil), declaration.Evidence...),
		Source:                "companion",
		LastSeenUnixMilli:     now.UnixMilli(),
		LeaseExpiresUnixMilli: now.Add(lease).UnixMilli(),
		Fresh:                 true,
	}

	targetMetadataLock.Lock()
	companionTarget = metadata
	targetMetadataLock.Unlock()

	scheduleCompanionTargetExpiryCheck(metadata.LeaseExpiresUnixMilli)
	return metadata
}

func getEffectiveTargetMetadata() TargetMetadata {
	targetMetadataLock.Lock()
	companion := companionTarget
	targetMetadataLock.Unlock()

	if companion.TargetType != "" && companion.LeaseExpiresUnixMilli > time.Now().UnixMilli() {
		companion.Fresh = true
		return companion
	}

	targetType := config.TargetType
	if targetType == "" {
		targetType = "generic"
	}
	return TargetMetadata{
		TargetType: targetType,
		Source:     "config",
		Fresh:      true,
	}
}

func clearCompanionTargetMetadata() {
	targetMetadataLock.Lock()
	defer targetMetadataLock.Unlock()
	companionTarget = TargetMetadata{}
}

func scheduleCompanionTargetExpiryCheck(leaseExpiresUnixMilli int64) {
	if leaseExpiresUnixMilli <= 0 {
		return
	}

	delay := time.Until(time.UnixMilli(leaseExpiresUnixMilli))
	if delay < 0 {
		delay = 0
	}

	go func() {
		time.Sleep(delay + time.Second)

		targetMetadataLock.Lock()
		expired := companionTarget.LeaseExpiresUnixMilli == leaseExpiresUnixMilli &&
			leaseExpiresUnixMilli <= time.Now().UnixMilli()
		targetMetadataLock.Unlock()

		if expired {
			logger.Warn().
				Int64("lease_expires_unix_milli", leaseExpiresUnixMilli).
				Msg("companion target lease expired")
			expireCompanionTargetMetadata(leaseExpiresUnixMilli)
		}
	}()
}

func expireCompanionTargetMetadata(leaseExpiresUnixMilli int64) {
	targetMetadataLock.Lock()
	defer targetMetadataLock.Unlock()

	if companionTarget.LeaseExpiresUnixMilli != leaseExpiresUnixMilli {
		return
	}
	companionTarget.Fresh = false
	companionTarget.LeaseExpiresUnixMilli = 0
}
