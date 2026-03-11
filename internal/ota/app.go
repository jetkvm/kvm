package ota

import (
	"context"
	"time"
)

const (
	appUpdatePath = "/userdata/jetkvm/jetkvm_app.update"
)

// DO NOT call it directly, it's not thread safe
// Mutex is currently held by the caller, e.g. doUpdate
func (s *State) updateApp(ctx context.Context, appUpdate *componentUpdateStatus, bypassSignatureCheck bool) error {
	l := s.l.With().Str("path", appUpdatePath).Logger()

	// Validate signature requirement and download if available
	signature, err := s.downloadComponentSignature(ctx, appUpdate, "app", &l, bypassSignatureCheck)
	if err != nil {
		return s.componentUpdateError("Error with app signature", err, &l)
	}

	if err := s.downloadFile(ctx, appUpdatePath, appUpdate.url, "app"); err != nil {
		return s.componentUpdateError("Error downloading app update", err, &l)
	}

	downloadFinished := time.Now()
	appUpdate.downloadFinishedAt = downloadFinished
	appUpdate.downloadProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	if err := s.verifyFile(
		ctx,
		appUpdatePath,
		appUpdate.hash,
		signature,
		&appUpdate.verificationProgress,
	); err != nil {
		return s.componentUpdateError("Error verifying app update", err, &l)
	}
	verifyFinished := time.Now()
	appUpdate.verifiedAt = verifyFinished
	appUpdate.verificationProgress = 1
	appUpdate.updatedAt = verifyFinished
	appUpdate.updateProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	l.Info().Msg("App update downloaded and verified")

	s.rebootNeeded = true

	return nil
}
