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
func (s *State) updateApp(ctx context.Context, appUpdate *componentUpdateStatus) error {
	l := s.l.With().Str("path", appUpdatePath).Logger()

	if err := s.downloadFile(ctx, appUpdatePath, appUpdate.url, "app"); err != nil {
		return s.componentUpdateError("Error downloading app update", err, &l)
	}

	downloadFinished := time.Now()
	appUpdate.downloadFinishedAt = downloadFinished
	appUpdate.downloadProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	if err := s.verifyFile(
		appUpdatePath,
		appUpdate.hash,
		&appUpdate.verificationProgress,
	); err != nil {
		return s.componentUpdateError("Error verifying app update hash", err, &l)
	}
	verifyFinished := time.Now()
	appUpdate.verifiedAt = verifyFinished
	appUpdate.verificationProgress = 1
	appUpdate.updatedAt = verifyFinished
	appUpdate.updateProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	l.Info().Msg("App update downloaded")

	s.rebootNeeded = true

	return nil
}
