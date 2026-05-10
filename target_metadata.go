package kvm

import (
	"sync"
	"time"
)

const companionTargetTTL = 45 * time.Second

type TargetMetadata struct {
	TargetType        string  `json:"target_type"`
	TargetMode        string  `json:"target_mode,omitempty"`
	DisplayWidth      int     `json:"display_width,omitempty"`
	DisplayHeight     int     `json:"display_height,omitempty"`
	DisplayAspect     float64 `json:"display_aspect,omitempty"`
	Source            string  `json:"source,omitempty"`
	LastSeenUnixMilli int64   `json:"last_seen_unix_milli,omitempty"`
	Fresh             bool    `json:"fresh"`
}

type CompanionTargetDeclaration struct {
	TargetType    string  `json:"target_type"`
	TargetMode    string  `json:"target_mode"`
	DisplayWidth  int     `json:"display_width"`
	DisplayHeight int     `json:"display_height"`
	DisplayAspect float64 `json:"display_aspect"`
}

var (
	targetMetadataLock sync.Mutex
	companionTarget    TargetMetadata
)

func setCompanionTargetMetadata(declaration CompanionTargetDeclaration) TargetMetadata {
	aspect := declaration.DisplayAspect
	if aspect <= 0 && declaration.DisplayWidth > 0 && declaration.DisplayHeight > 0 {
		aspect = float64(declaration.DisplayWidth) / float64(declaration.DisplayHeight)
	}

	targetMetadataLock.Lock()
	defer targetMetadataLock.Unlock()

	companionTarget = TargetMetadata{
		TargetType:        declaration.TargetType,
		TargetMode:        declaration.TargetMode,
		DisplayWidth:      declaration.DisplayWidth,
		DisplayHeight:     declaration.DisplayHeight,
		DisplayAspect:     aspect,
		Source:            "companion",
		LastSeenUnixMilli: time.Now().UnixMilli(),
		Fresh:             true,
	}
	return companionTarget
}

func getEffectiveTargetMetadata() TargetMetadata {
	targetMetadataLock.Lock()
	companion := companionTarget
	targetMetadataLock.Unlock()

	if companion.TargetType != "" && time.Since(time.UnixMilli(companion.LastSeenUnixMilli)) <= companionTargetTTL {
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
