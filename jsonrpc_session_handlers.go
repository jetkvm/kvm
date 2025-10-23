package kvm

import (
	"errors"
	"fmt"
)

// handleSessionTransferRPC handles primary control transfer requests (approve/deny)
func handleSessionTransferRPC(method string, params map[string]any, session *Session) (any, error) {
	requesterID, ok := params["requesterID"].(string)
	if !ok {
		return nil, errors.New("invalid requesterID parameter")
	}

	if err := RequirePermission(session, PermissionSessionTransfer); err != nil {
		return nil, err
	}

	var err error
	switch method {
	case "approvePrimaryRequest":
		err = sessionManager.ApprovePrimaryRequest(session.ID, requesterID)
		if err == nil {
			return map[string]interface{}{"status": "approved"}, nil
		}
	case "denyPrimaryRequest":
		err = sessionManager.DenyPrimaryRequest(session.ID, requesterID)
		if err == nil {
			return map[string]interface{}{"status": "denied"}, nil
		}
	}
	return nil, err
}

// handleSessionApprovalRPC handles new session approval requests (approve/deny)
func handleSessionApprovalRPC(method string, params map[string]any, session *Session) (any, error) {
	sessionID, ok := params["sessionId"].(string)
	if !ok {
		return nil, errors.New("invalid sessionId parameter")
	}

	if err := RequirePermission(session, PermissionSessionApprove); err != nil {
		return nil, err
	}

	var err error
	switch method {
	case "approveNewSession":
		err = sessionManager.ApproveSession(sessionID)
		if err == nil {
			go sessionManager.broadcastSessionListUpdate()
			return map[string]interface{}{"status": "approved"}, nil
		}
	case "denyNewSession":
		err = sessionManager.DenySession(sessionID)
		if err == nil {
			if targetSession := sessionManager.GetSession(sessionID); targetSession != nil {
				go func() {
					writeJSONRPCEvent("sessionAccessDenied", map[string]interface{}{
						"message": "Access denied by primary session",
					}, targetSession)
					sessionManager.broadcastSessionListUpdate()
				}()
			}
			return map[string]interface{}{"status": "denied"}, nil
		}
	}
	return nil, err
}

// handleRequestSessionApprovalRPC handles pending sessions requesting approval from primary
func handleRequestSessionApprovalRPC(session *Session) (any, error) {
	if session.Mode != SessionModePending {
		return nil, errors.New("only pending sessions can request approval")
	}

	if currentSessionSettings == nil || !currentSessionSettings.RequireApproval {
		return nil, errors.New("session approval not required")
	}

	primary := sessionManager.GetPrimarySession()
	if primary == nil {
		return nil, errors.New("no primary session available")
	}

	go func() {
		writeJSONRPCEvent("newSessionPending", map[string]interface{}{
			"sessionId": session.ID,
			"source":    session.Source,
			"identity":  session.Identity,
			"nickname":  session.Nickname,
		}, primary)
	}()

	return map[string]interface{}{"status": "requested"}, nil
}

func validateNickname(nickname string) error {
	if len(nickname) < minNicknameLength {
		return fmt.Errorf("nickname must be at least %d characters", minNicknameLength)
	}
	if len(nickname) > maxNicknameLength {
		return fmt.Errorf("nickname must be %d characters or less", maxNicknameLength)
	}
	if !isValidNickname(nickname) {
		return errors.New("nickname can only contain letters, numbers, spaces, and - _ . @")
	}

	for i, r := range nickname {
		if r < 32 || r == 127 {
			return fmt.Errorf("nickname contains control character at position %d", i)
		}
		if r >= 0x200B && r <= 0x200D {
			return errors.New("nickname contains zero-width character")
		}
	}

	trimmed := ""
	for _, r := range nickname {
		trimmed += string(r)
	}
	if trimmed != nickname {
		return errors.New("nickname contains disallowed unicode")
	}

	return nil
}

func handleUpdateSessionNicknameRPC(params map[string]any, session *Session) (any, error) {
	sessionID, _ := params["sessionId"].(string)
	nickname, _ := params["nickname"].(string)

	if err := validateNickname(nickname); err != nil {
		return nil, err
	}

	targetSession := sessionManager.GetSession(sessionID)
	if targetSession == nil {
		return nil, errors.New("session not found")
	}

	if targetSession.ID != session.ID && !session.HasPermission(PermissionSessionManage) {
		return nil, errors.New("permission denied: can only update own nickname")
	}

	if err := sessionManager.UpdateSessionNickname(sessionID, nickname); err != nil {
		return nil, err
	}

	// If session is pending and approval is required, send the approval request now that we have a nickname
	if targetSession.Mode == SessionModePending && currentSessionSettings != nil && currentSessionSettings.RequireApproval {
		if primary := sessionManager.GetPrimarySession(); primary != nil {
			go func() {
				writeJSONRPCEvent("newSessionPending", map[string]interface{}{
					"sessionId": targetSession.ID,
					"source":    targetSession.Source,
					"identity":  targetSession.Identity,
					"nickname":  targetSession.Nickname,
				}, primary)
			}()
		}
	}

	sessionManager.broadcastSessionListUpdate()
	return map[string]interface{}{"status": "updated"}, nil
}

// handleGetPermissionsRPC returns permissions for the current session
func handleGetPermissionsRPC(session *Session) (any, error) {
	permissions := session.GetPermissions()
	permMap := make(map[string]bool)
	for perm, allowed := range permissions {
		permMap[string(perm)] = allowed
	}
	return GetPermissionsResponse{
		Mode:        string(session.Mode),
		Permissions: permMap,
	}, nil
}

// handleSessionSettingsRPC handles getting or setting session settings
func handleSessionSettingsRPC(method string, params map[string]any, session *Session) (any, error) {
	switch method {
	case "getSessionSettings":
		if err := RequirePermission(session, PermissionSettingsRead); err != nil {
			return nil, err
		}
		return currentSessionSettings, nil

	case "setSessionSettings":
		if err := RequirePermission(session, PermissionSessionManage); err != nil {
			return nil, err
		}

		settings, ok := params["settings"].(map[string]interface{})
		if !ok {
			return nil, errors.New("invalid settings parameter")
		}

		if requireApproval, ok := settings["requireApproval"].(bool); ok {
			currentSessionSettings.RequireApproval = requireApproval
		}
		if requireNickname, ok := settings["requireNickname"].(bool); ok {
			currentSessionSettings.RequireNickname = requireNickname
		}
		if reconnectGrace, ok := settings["reconnectGrace"].(float64); ok {
			currentSessionSettings.ReconnectGrace = int(reconnectGrace)
		}
		if primaryTimeout, ok := settings["primaryTimeout"].(float64); ok {
			currentSessionSettings.PrimaryTimeout = int(primaryTimeout)
		}
		if privateKeystrokes, ok := settings["privateKeystrokes"].(bool); ok {
			currentSessionSettings.PrivateKeystrokes = privateKeystrokes
		}
		if maxRejectionAttempts, ok := settings["maxRejectionAttempts"].(float64); ok {
			currentSessionSettings.MaxRejectionAttempts = int(maxRejectionAttempts)
		}
		if maxSessions, ok := settings["maxSessions"].(float64); ok {
			currentSessionSettings.MaxSessions = int(maxSessions)
		}
		if observerTimeout, ok := settings["observerTimeout"].(float64); ok {
			currentSessionSettings.ObserverTimeout = int(observerTimeout)
		}

		if sessionManager != nil {
			sessionManager.updateAllSessionNicknames()
		}

		if err := SaveConfig(); err != nil {
			return nil, errors.New("failed to save session settings")
		}
		return currentSessionSettings, nil
	}

	return nil, fmt.Errorf("unknown session settings method: %s", method)
}

// handleGenerateNicknameRPC generates a nickname based on user agent
func handleGenerateNicknameRPC(params map[string]any) (any, error) {
	userAgent := ""
	if params != nil {
		if ua, ok := params["userAgent"].(string); ok {
			userAgent = ua
		}
	}

	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Unknown) Browser"
	}

	return map[string]string{
		"nickname": generateNicknameFromUserAgent(userAgent),
	}, nil
}
