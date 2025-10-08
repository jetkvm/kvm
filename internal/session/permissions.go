package session

import "fmt"

// Permission represents a specific action that can be performed
type Permission string

const (
	// Video/Display permissions
	PermissionVideoView Permission = "video.view"

	// Input permissions
	PermissionKeyboardInput Permission = "keyboard.input"
	PermissionMouseInput    Permission = "mouse.input"
	PermissionPaste         Permission = "clipboard.paste"

	// Session management permissions
	PermissionSessionTransfer      Permission = "session.transfer"
	PermissionSessionApprove       Permission = "session.approve"
	PermissionSessionKick          Permission = "session.kick"
	PermissionSessionRequestPrimary Permission = "session.request_primary"
	PermissionSessionReleasePrimary Permission = "session.release_primary"
	PermissionSessionManage         Permission = "session.manage"

	// Power/USB control permissions
	PermissionPowerControl Permission = "power.control"
	PermissionUSBControl   Permission = "usb.control"

	// Mount/Media permissions
	PermissionMountMedia   Permission = "mount.media"
	PermissionUnmountMedia Permission = "mount.unmedia"
	PermissionMountList    Permission = "mount.list"

	// Extension permissions
	PermissionExtensionManage Permission = "extension.manage"

	// Terminal/Serial permissions
	PermissionTerminalAccess Permission = "terminal.access"
	PermissionSerialAccess   Permission = "serial.access"
	PermissionExtensionATX    Permission = "extension.atx"
	PermissionExtensionDC     Permission = "extension.dc"
	PermissionExtensionSerial Permission = "extension.serial"
	PermissionExtensionWOL    Permission = "extension.wol"

	// Settings permissions
	PermissionSettingsRead   Permission = "settings.read"
	PermissionSettingsWrite  Permission = "settings.write"
	PermissionSettingsAccess Permission = "settings.access" // Access control settings

	// System permissions
	PermissionSystemReboot  Permission = "system.reboot"
	PermissionSystemUpdate  Permission = "system.update"
	PermissionSystemNetwork Permission = "system.network"
)

// PermissionSet represents a set of permissions
type PermissionSet map[Permission]bool

// RolePermissions defines permissions for each session mode
var RolePermissions = map[SessionMode]PermissionSet{
	SessionModePrimary: {
		// Primary has all permissions
		PermissionVideoView:             true,
		PermissionKeyboardInput:         true,
		PermissionMouseInput:            true,
		PermissionPaste:                 true,
		PermissionSessionTransfer:       true,
		PermissionSessionApprove:        true,
		PermissionSessionKick:           true,
		PermissionSessionReleasePrimary: true,
		PermissionMountMedia:            true,
		PermissionUnmountMedia:          true,
		PermissionMountList:             true,
		PermissionExtensionManage:       true,
		PermissionExtensionATX:          true,
		PermissionExtensionDC:           true,
		PermissionExtensionSerial:       true,
		PermissionExtensionWOL:          true,
		PermissionSettingsRead:          true,
		PermissionSettingsWrite:         true,
		PermissionSettingsAccess:        true,  // Only primary can access settings UI
		PermissionSystemReboot:          true,
		PermissionSystemUpdate:          true,
		PermissionSystemNetwork:         true,
		PermissionTerminalAccess:        true,
		PermissionSerialAccess:          true,
		PermissionPowerControl:          true,
		PermissionUSBControl:            true,
		PermissionSessionManage:         true,
		PermissionSessionRequestPrimary: false, // Primary doesn't need to request
	},
	SessionModeObserver: {
		// Observers can only view
		PermissionVideoView:             true,
		PermissionSessionRequestPrimary: true,
		PermissionMountList:             true, // Can see what's mounted but not mount/unmount
	},
	SessionModeQueued: {
		// Queued sessions can view and request primary
		PermissionVideoView:             true,
		PermissionSessionRequestPrimary: true,
	},
	SessionModePending: {
		// Pending sessions have minimal permissions
		PermissionVideoView: true,
	},
}

// CheckPermission checks if a session mode has a specific permission
func CheckPermission(mode SessionMode, perm Permission) bool {
	permissions, exists := RolePermissions[mode]
	if !exists {
		return false
	}
	return permissions[perm]
}

// GetPermissionsForMode returns all permissions for a session mode
func GetPermissionsForMode(mode SessionMode) PermissionSet {
	permissions, exists := RolePermissions[mode]
	if !exists {
		return PermissionSet{}
	}

	// Return a copy to prevent modification
	result := make(PermissionSet)
	for k, v := range permissions {
		result[k] = v
	}
	return result
}

// RequirePermissionForMode is a middleware-like function for RPC handlers
func RequirePermissionForMode(mode SessionMode, perm Permission) error {
	if !CheckPermission(mode, perm) {
		return fmt.Errorf("permission denied: %s", perm)
	}
	return nil
}

// GetPermissionsResponse is the response structure for getPermissions RPC
type GetPermissionsResponse struct {
	Mode        string            `json:"mode"`
	Permissions map[string]bool   `json:"permissions"`
}

// MethodPermissions maps RPC methods to required permissions
var MethodPermissions = map[string]Permission{
	// Power/hardware control
	"setATXPowerAction":      PermissionPowerControl,
	"setDCPowerState":        PermissionPowerControl,
	"setDCRestoreState":      PermissionPowerControl,

	// USB device control
	"setUsbDeviceState":      PermissionUSBControl,
	"setUsbDevices":          PermissionUSBControl,

	// Mount operations
	"mountUsb":               PermissionMountMedia,
	"unmountUsb":             PermissionMountMedia,
	"mountBuiltInImage":      PermissionMountMedia,
	"rpcMountBuiltInImage":   PermissionMountMedia,
	"unmountImage":           PermissionMountMedia,
	"mountWithHTTP":          PermissionMountMedia,
	"mountWithStorage":       PermissionMountMedia,
	"checkMountUrl":          PermissionMountMedia,
	"startStorageFileUpload": PermissionMountMedia,
	"deleteStorageFile":      PermissionMountMedia,

	// Settings operations
	"setDevModeState":        PermissionSettingsWrite,
	"setDevChannelState":     PermissionSettingsWrite,
	"setAutoUpdateState":     PermissionSettingsWrite,
	"tryUpdate":              PermissionSettingsWrite,
	"reboot":                 PermissionSettingsWrite,
	"resetConfig":            PermissionSettingsWrite,
	"setNetworkSettings":     PermissionSettingsWrite,
	"setLocalLoopbackOnly":   PermissionSettingsWrite,
	"renewDHCPLease":         PermissionSettingsWrite,
	"setSSHKeyState":         PermissionSettingsWrite,
	"setTLSState":            PermissionSettingsWrite,
	"setVideoBandwidth":      PermissionSettingsWrite,
	"setVideoFramerate":      PermissionSettingsWrite,
	"setVideoResolution":     PermissionSettingsWrite,
	"setVideoEncoderQuality": PermissionSettingsWrite,
	"setVideoSignal":         PermissionSettingsWrite,
	"setSerialBitrate":       PermissionSettingsWrite,
	"setSerialSettings":      PermissionSettingsWrite,
	"setSessionSettings":     PermissionSessionManage,
	"updateSessionSettings":  PermissionSessionManage,

	// Display settings
	"setEDID":                PermissionSettingsWrite,
	"setStreamQualityFactor": PermissionSettingsWrite,
	"setDisplayRotation":     PermissionSettingsWrite,
	"setBacklightSettings":   PermissionSettingsWrite,

	// USB/HID settings
	"setUsbEmulationState":   PermissionSettingsWrite,
	"setUsbConfig":           PermissionSettingsWrite,
	"setKeyboardLayout":      PermissionSettingsWrite,
	"setJigglerState":        PermissionSettingsWrite,
	"setJigglerConfig":       PermissionSettingsWrite,
	"setMassStorageMode":     PermissionSettingsWrite,
	"setKeyboardMacros":      PermissionSettingsWrite,
	"setWakeOnLanDevices":    PermissionSettingsWrite,

	// Cloud settings
	"setCloudUrl":            PermissionSettingsWrite,
	"deregisterDevice":       PermissionSettingsWrite,

	// Active extension control
	"setActiveExtension":     PermissionExtensionManage,

	// Input operations (already handled in other places but for consistency)
	"keyboardReport":         PermissionKeyboardInput,
	"keypressReport":         PermissionKeyboardInput,
	"absMouseReport":         PermissionMouseInput,
	"relMouseReport":         PermissionMouseInput,
	"wheelReport":            PermissionMouseInput,
	"executeKeyboardMacro":   PermissionPaste,
	"cancelKeyboardMacro":    PermissionPaste,

	// Session operations
	"approveNewSession":      PermissionSessionApprove,
	"denyNewSession":         PermissionSessionApprove,
	"transferSession":        PermissionSessionTransfer,
	"transferPrimary":        PermissionSessionTransfer,
	"requestPrimary":         PermissionSessionRequestPrimary,
	"releasePrimary":         PermissionSessionReleasePrimary,

	// Extension operations
	"activateExtension":      PermissionExtensionManage,
	"deactivateExtension":    PermissionExtensionManage,
	"sendWOLMagicPacket":     PermissionExtensionWOL,

	// Read operations - require appropriate read permissions
	"getSessionSettings":     PermissionSettingsRead,
	"getSessionConfig":       PermissionSettingsRead,
	"getSessionData":         PermissionVideoView,
	"getNetworkSettings":     PermissionSettingsRead,
	"getSerialSettings":      PermissionSettingsRead,
	"getBacklightSettings":   PermissionSettingsRead,
	"getDisplayRotation":     PermissionSettingsRead,
	"getEDID":                PermissionSettingsRead,
	"get_edid":               PermissionSettingsRead,
	"getKeyboardLayout":      PermissionSettingsRead,
	"getJigglerConfig":       PermissionSettingsRead,
	"getJigglerState":        PermissionSettingsRead,
	"getStreamQualityFactor": PermissionSettingsRead,
	"getVideoSettings":       PermissionSettingsRead,
	"getVideoBandwidth":      PermissionSettingsRead,
	"getVideoFramerate":      PermissionSettingsRead,
	"getVideoResolution":     PermissionSettingsRead,
	"getVideoEncoderQuality": PermissionSettingsRead,
	"getVideoSignal":         PermissionSettingsRead,
	"getSerialBitrate":       PermissionSettingsRead,
	"getDevModeState":        PermissionSettingsRead,
	"getDevChannelState":     PermissionSettingsRead,
	"getAutoUpdateState":     PermissionSettingsRead,
	"getLocalLoopbackOnly":   PermissionSettingsRead,
	"getSSHKeyState":         PermissionSettingsRead,
	"getTLSState":            PermissionSettingsRead,
	"getCloudUrl":            PermissionSettingsRead,
	"getCloudState":          PermissionSettingsRead,
	"getNetworkState":        PermissionSettingsRead,

	// Mount/media read operations
	"getMassStorageMode":     PermissionMountList,
	"getUsbState":            PermissionMountList,
	"getUSBState":            PermissionMountList,
	"listStorageFiles":       PermissionMountList,
	"getStorageSpace":        PermissionMountList,

	// Extension read operations
	"getActiveExtension":     PermissionSettingsRead,

	// Power state reads
	"getATXState":            PermissionSettingsRead,
	"getDCPowerState":        PermissionSettingsRead,
	"getDCRestoreState":      PermissionSettingsRead,

	// Device info reads (these should be accessible to all)
	"getDeviceID":            PermissionVideoView,
	"getLocalVersion":        PermissionVideoView,
	"getVideoState":          PermissionVideoView,
	"getKeyboardLedState":    PermissionVideoView,
	"getKeyDownState":        PermissionVideoView,
	"ping":                   PermissionVideoView,
	"getTimezones":           PermissionVideoView,
	"getSessions":            PermissionVideoView,
	"getUpdateStatus":        PermissionSettingsRead,
	"isUpdatePending":        PermissionSettingsRead,
	"getUsbEmulationState":   PermissionSettingsRead,
	"getUsbConfig":           PermissionSettingsRead,
	"getUsbDevices":          PermissionSettingsRead,
	"getKeyboardMacros":      PermissionSettingsRead,
	"getWakeOnLanDevices":    PermissionSettingsRead,
	"getVirtualMediaState":   PermissionMountList,
}

// GetMethodPermission returns the required permission for an RPC method
func GetMethodPermission(method string) (Permission, bool) {
	perm, exists := MethodPermissions[method]
	return perm, exists
}