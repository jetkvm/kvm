package kvm

import (
	"github.com/jetkvm/kvm/internal/session"
)

type (
	Permission    = session.Permission
	PermissionSet = session.PermissionSet
	SessionMode   = session.SessionMode
)

const (
	SessionModePrimary  = session.SessionModePrimary
	SessionModeObserver = session.SessionModeObserver
	SessionModeQueued   = session.SessionModeQueued
	SessionModePending  = session.SessionModePending

	PermissionVideoView             = session.PermissionVideoView
	PermissionKeyboardInput         = session.PermissionKeyboardInput
	PermissionMouseInput            = session.PermissionMouseInput
	PermissionPaste                 = session.PermissionPaste
	PermissionSessionTransfer       = session.PermissionSessionTransfer
	PermissionSessionApprove        = session.PermissionSessionApprove
	PermissionSessionKick           = session.PermissionSessionKick
	PermissionSessionRequestPrimary = session.PermissionSessionRequestPrimary
	PermissionSessionReleasePrimary = session.PermissionSessionReleasePrimary
	PermissionSessionManage         = session.PermissionSessionManage
	PermissionPowerControl          = session.PermissionPowerControl
	PermissionUSBControl            = session.PermissionUSBControl
	PermissionMountMedia            = session.PermissionMountMedia
	PermissionUnmountMedia          = session.PermissionUnmountMedia
	PermissionMountList             = session.PermissionMountList
	PermissionExtensionManage       = session.PermissionExtensionManage
	PermissionExtensionATX          = session.PermissionExtensionATX
	PermissionExtensionDC           = session.PermissionExtensionDC
	PermissionExtensionSerial       = session.PermissionExtensionSerial
	PermissionExtensionWOL          = session.PermissionExtensionWOL
	PermissionTerminalAccess        = session.PermissionTerminalAccess
	PermissionSerialAccess          = session.PermissionSerialAccess
	PermissionSettingsRead          = session.PermissionSettingsRead
	PermissionSettingsWrite         = session.PermissionSettingsWrite
	PermissionSettingsAccess        = session.PermissionSettingsAccess
	PermissionSystemReboot          = session.PermissionSystemReboot
	PermissionSystemUpdate          = session.PermissionSystemUpdate
	PermissionSystemNetwork         = session.PermissionSystemNetwork
)

var (
	GetMethodPermission = session.GetMethodPermission
)

type GetPermissionsResponse = session.GetPermissionsResponse

func (s *Session) HasPermission(perm Permission) bool {
	if s == nil {
		return false
	}
	return session.CheckPermission(s.Mode, perm)
}

func (s *Session) GetPermissions() PermissionSet {
	if s == nil {
		return PermissionSet{}
	}
	return session.GetPermissionsForMode(s.Mode)
}

func RequirePermission(s *Session, perm Permission) error {
	if s == nil {
		return session.RequirePermissionForMode(SessionModePending, perm)
	}
	if !s.HasPermission(perm) {
		return session.RequirePermissionForMode(s.Mode, perm)
	}
	return nil
}
