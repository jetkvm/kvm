package kvm

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/native"
)

const dynamicDisplayRefreshHz = 60

type DisplayMode struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	RefreshHz int    `json:"refresh_hz"`
	Source    string `json:"source,omitempty"`
}

type DisplayModeStatus struct {
	CompanionTarget       TargetMetadata    `json:"companion_target"`
	AdvertisedMode        *DisplayMode      `json:"advertised_edid_mode,omitempty"`
	ActualInputMode       native.VideoState `json:"actual_hdmi_input_mode"`
	ActiveVideoStreamMode native.VideoState `json:"active_webrtc_video_stream"`
}

var dynamicDisplayModeState = struct {
	sync.Mutex
	mode *DisplayMode
	edid string
}{}

func applyDisplayModeForTarget(metadata TargetMetadata) {
	if metadata.TargetType != "android" || !metadata.Fresh || metadata.DisplayWidth <= 0 || metadata.DisplayHeight <= 0 {
		if metadata.TargetType == "android" && metadata.Source == "companion" {
			applyDefaultEDIDFallback("companion target inactive", true)
		}
		return
	}

	mode := selectCompanionDisplayMode(metadata)
	edid, err := buildDynamicDisplayEDID(mode)
	if err != nil {
		logger.Warn().
			Err(err).
			Int("display_width", metadata.DisplayWidth).
			Int("display_height", metadata.DisplayHeight).
			Msg("failed to build companion display EDID, restoring default EDID")
		applyDefaultEDIDFallback("dynamic EDID build failed", true)
		return
	}

	dynamicDisplayModeState.Lock()
	alreadyApplied := dynamicDisplayModeState.edid == edid
	dynamicDisplayModeState.Unlock()
	if alreadyApplied {
		return
	}

	logger.Warn().
		Int("width", mode.Width).
		Int("height", mode.Height).
		Int("refresh_hz", mode.RefreshHz).
		Str("source", mode.Source).
		Int("target_width", metadata.DisplayWidth).
		Int("target_height", metadata.DisplayHeight).
		Float64("target_aspect", metadata.DisplayAspect).
		Int("edid_bytes", len(edid)/2).
		Msg("applying companion-derived EDID display mode")

	if err := nativeInstance.VideoSetEDID(edid); err != nil {
		logger.Warn().
			Err(err).
			Int("width", mode.Width).
			Int("height", mode.Height).
			Int("refresh_hz", mode.RefreshHz).
			Msg("failed to apply companion-derived EDID display mode")
		return
	}

	reenumerationRequired := config.EdidString != edid
	if reenumerationRequired {
		config.EdidString = edid
		if err := SaveConfig(); err != nil {
			logger.Warn().
				Err(err).
				Int("width", mode.Width).
				Int("height", mode.Height).
				Int("refresh_hz", mode.RefreshHz).
				Msg("failed to persist companion-derived EDID display mode")
			return
		}
		logger.Warn().
			Int("width", mode.Width).
			Int("height", mode.Height).
			Int("refresh_hz", mode.RefreshHz).
			Msg("persisted companion-derived EDID display mode")
	}

	dynamicDisplayModeState.Lock()
	dynamicDisplayModeState.mode = &mode
	dynamicDisplayModeState.edid = edid
	dynamicDisplayModeState.Unlock()

	if reenumerationRequired {
		requestDisplayModeReenumeration("companion display mode applied")
	}
}

func applyDefaultEDIDFallback(reason string, reenumerate bool) {
	defaultEDID := getDeviceDefaultEDID()

	dynamicDisplayModeState.Lock()
	hadDynamicMode := dynamicDisplayModeState.edid != ""
	dynamicDisplayModeState.mode = nil
	dynamicDisplayModeState.edid = ""
	dynamicDisplayModeState.Unlock()

	logger.Warn().
		Bool("had_dynamic_mode", hadDynamicMode).
		Str("reason", reason).
		Msg("restoring default EDID fallback")
	if err := nativeInstance.VideoSetEDID(defaultEDID); err != nil {
		logger.Warn().Err(err).Msg("failed to restore default EDID fallback")
		return
	}

	reenumerationRequired := reenumerate && (hadDynamicMode || config.EdidString != defaultEDID)
	if config.EdidString != defaultEDID {
		config.EdidString = defaultEDID
		if err := SaveConfig(); err != nil {
			logger.Warn().Err(err).Msg("failed to persist default EDID fallback")
			return
		}
	}

	if reenumerationRequired {
		requestDisplayModeReenumeration("default EDID restored")
	}
}

func requestDisplayModeReenumeration(reason string) {
	logger.Warn().Str("reason", reason).Msg("rebooting JetKVM to force HDMI re-enumeration")
	go func() {
		if err := hwReboot(true, nil, 3*time.Second); err != nil {
			logger.Warn().Err(err).Str("reason", reason).Msg("failed to reboot JetKVM for HDMI re-enumeration")
		}
	}()
}

func selectCompanionDisplayMode(metadata TargetMetadata) DisplayMode {
	width := metadata.DisplayWidth
	height := metadata.DisplayHeight
	source := "companion"

	const maxDynamicDisplayLongEdge = 1600
	longEdge := maxInt(width, height)
	if longEdge > maxDynamicDisplayLongEdge {
		width = roundToMultiple(maxInt(1, width*maxDynamicDisplayLongEdge/longEdge), 8)
		height = roundToMultiple(maxInt(1, height*maxDynamicDisplayLongEdge/longEdge), 8)
		source = "companion-aspect-scaled"
	}

	return DisplayMode{
		Width:     width,
		Height:    height,
		RefreshHz: dynamicDisplayRefreshHz,
		Source:    source,
	}
}

func getDisplayModeStatus() DisplayModeStatus {
	dynamicDisplayModeState.Lock()
	var mode *DisplayMode
	if dynamicDisplayModeState.mode != nil {
		modeCopy := *dynamicDisplayModeState.mode
		mode = &modeCopy
	}
	dynamicDisplayModeState.Unlock()

	return DisplayModeStatus{
		CompanionTarget:       getEffectiveTargetMetadata(),
		AdvertisedMode:        mode,
		ActualInputMode:       lastVideoState,
		ActiveVideoStreamMode: lastVideoState,
	}
}

func buildDynamicDisplayEDID(mode DisplayMode) (string, error) {
	if mode.Width <= 0 || mode.Height <= 0 {
		return "", fmt.Errorf("invalid display mode dimensions %dx%d", mode.Width, mode.Height)
	}
	if mode.Width > 4095 || mode.Height > 4095 {
		return "", fmt.Errorf("display mode exceeds EDID detailed timing limit: %dx%d", mode.Width, mode.Height)
	}
	refreshHz := mode.RefreshHz
	if refreshHz <= 0 {
		refreshHz = dynamicDisplayRefreshHz
	}

	defaultEDID, err := hex.DecodeString(getDeviceDefaultEDID())
	if err != nil {
		return "", fmt.Errorf("decode device default EDID: %w", err)
	}
	if len(defaultEDID) < 128 {
		return "", fmt.Errorf("unexpected device default EDID length %d", len(defaultEDID))
	}

	dtd, err := buildDetailedTimingDescriptor(mode.Width, mode.Height, refreshHz)
	if err != nil {
		return "", err
	}

	edid := buildDisplayEDID(defaultEDID[:128], dtd, mode.Width, mode.Height)
	fixEDIDChecksums(edid)
	return hex.EncodeToString(edid), nil
}

func buildDisplayEDID(defaultEDID, preferredDTD []byte, width, height int) []byte {
	edid := make([]byte, 128)
	copy(edid, defaultEDID)

	hImageMM, vImageMM := imageSizeMM(width, height)
	edid[21] = byte(maxInt(1, hImageMM/10))
	edid[22] = byte(maxInt(1, vImageMM/10))

	// Do not leave stock 16:9 timings in the generated EDID. Android otherwise
	// keeps the stock modes as physical modes and only synthesizes anisotropic
	// logical modes from the changed physical size.
	for i := 35; i <= 37; i++ {
		edid[i] = 0
	}
	for i := 38; i <= 53; i++ {
		edid[i] = 0x01
	}
	copy(edid[54:72], preferredDTD)
	copy(edid[72:90], displayRangeDescriptor(preferredDTD))
	copy(edid[90:108], monitorNameDescriptor(findEDIDTextDescriptor(defaultEDID, 0xfc)))
	copy(edid[108:126], monitorSerialDescriptor(findEDIDTextDescriptor(defaultEDID, 0xff)))
	edid[126] = 0

	return edid
}

func buildDetailedTimingDescriptor(width, height, refreshHz int) ([]byte, error) {
	hFrontPorch := maxInt(48, roundToMultiple(width/20, 8))
	hSyncWidth := maxInt(32, roundToMultiple(width/10, 8))
	hBlank := maxInt(320, roundToMultiple(width*3/10, 8))
	if hBlank < hFrontPorch+hSyncWidth+32 {
		hBlank = hFrontPorch + hSyncWidth + 32
	}

	vFrontPorch := 3
	vSyncWidth := 5
	vBlank := maxInt(30, height/50)
	if vBlank < vFrontPorch+vSyncWidth+8 {
		vBlank = vFrontPorch + vSyncWidth + 8
	}

	pixelClock10KHz := ((width + hBlank) * (height + vBlank) * refreshHz) / 10000
	if pixelClock10KHz <= 0 || pixelClock10KHz > 0xffff {
		return nil, fmt.Errorf("display mode pixel clock out of EDID range: %dx%d@%d", width, height, refreshHz)
	}

	hImageMM, vImageMM := imageSizeMM(width, height)
	dtd := make([]byte, 18)
	dtd[0] = byte(pixelClock10KHz)
	dtd[1] = byte(pixelClock10KHz >> 8)
	dtd[2] = byte(width)
	dtd[3] = byte(hBlank)
	dtd[4] = byte(((width >> 8) & 0x0f) << 4)
	dtd[4] |= byte((hBlank >> 8) & 0x0f)
	dtd[5] = byte(height)
	dtd[6] = byte(vBlank)
	dtd[7] = byte(((height >> 8) & 0x0f) << 4)
	dtd[7] |= byte((vBlank >> 8) & 0x0f)
	dtd[8] = byte(hFrontPorch)
	dtd[9] = byte(hSyncWidth)
	dtd[10] = byte((vFrontPorch & 0x0f) << 4)
	dtd[10] |= byte(vSyncWidth & 0x0f)
	dtd[11] = byte(((hFrontPorch >> 8) & 0x03) << 6)
	dtd[11] |= byte(((hSyncWidth >> 8) & 0x03) << 4)
	dtd[11] |= byte(((vFrontPorch >> 4) & 0x03) << 2)
	dtd[11] |= byte((vSyncWidth >> 4) & 0x03)
	dtd[12] = byte(hImageMM)
	dtd[13] = byte(vImageMM)
	dtd[14] = byte(((hImageMM >> 8) & 0x0f) << 4)
	dtd[14] |= byte((vImageMM >> 8) & 0x0f)
	dtd[17] = 0x1e
	return dtd, nil
}

func displayRangeDescriptor(preferredDTD []byte) []byte {
	pixelClockMHz := int(preferredDTD[0]) | int(preferredDTD[1])<<8
	pixelClockMHz = (pixelClockMHz + 99) / 100
	if pixelClockMHz < 10 {
		pixelClockMHz = 10
	}
	if pixelClockMHz > 255 {
		pixelClockMHz = 255
	}

	return []byte{
		0x00, 0x00, 0x00, 0xfd, 0x00,
		20, 75,
		15, 160,
		byte(pixelClockMHz),
		0x0a, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	}
}

func monitorNameDescriptor(defaultDescriptor []byte) []byte {
	if len(defaultDescriptor) == 18 && defaultDescriptor[0] == 0 && defaultDescriptor[1] == 0 && defaultDescriptor[3] == 0xfc {
		out := make([]byte, 18)
		copy(out, defaultDescriptor)
		return out
	}
	return textDescriptor(0xfc, "JKVM Dynamic")
}

func monitorSerialDescriptor(defaultDescriptor []byte) []byte {
	if len(defaultDescriptor) == 18 && defaultDescriptor[0] == 0 && defaultDescriptor[1] == 0 && defaultDescriptor[3] == 0xff {
		out := make([]byte, 18)
		copy(out, defaultDescriptor)
		return out
	}
	return textDescriptor(0xff, "JetKVM")
}

func textDescriptor(tag byte, text string) []byte {
	out := []byte{
		0x00, 0x00, 0x00, tag, 0x00,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	}
	for i := 0; i < len(text) && i < 13; i++ {
		out[5+i] = text[i]
	}
	if len(text) < 13 {
		out[5+len(text)] = 0x0a
	}
	return out
}

func findEDIDTextDescriptor(edid []byte, tag byte) []byte {
	const descriptorLength = 18
	for i := 54; i+descriptorLength <= len(edid) && i < 126; i += descriptorLength {
		if edid[i] == 0x00 && edid[i+1] == 0x00 && edid[i+2] == 0x00 && edid[i+3] == tag && edid[i+4] == 0x00 {
			out := make([]byte, descriptorLength)
			copy(out, edid[i:i+descriptorLength])
			return out
		}
	}
	return nil
}

func imageSizeMM(width, height int) (int, int) {
	const longEdgeMM = 160
	if width >= height {
		return longEdgeMM, maxInt(1, height*longEdgeMM/width)
	}
	return maxInt(1, width*longEdgeMM/height), longEdgeMM
}

func fixEDIDChecksums(edid []byte) {
	for block := 0; block+127 < len(edid); block += 128 {
		sum := 0
		for i := 0; i < 127; i++ {
			sum += int(edid[block+i])
		}
		edid[block+127] = byte((256 - (sum % 256)) % 256)
	}
}

func roundUp(value, multiple int) int {
	if multiple <= 0 || value == 0 {
		return value
	}
	remainder := value % multiple
	if remainder == 0 {
		return value
	}
	return value + multiple - remainder
}

func roundToMultiple(value, multiple int) int {
	if multiple <= 0 {
		return value
	}
	rounded := ((value + multiple/2) / multiple) * multiple
	if rounded < multiple {
		return multiple
	}
	return rounded
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
