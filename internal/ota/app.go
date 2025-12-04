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
	logger := GetOtaLoggingContext().Str("path", appUpdatePath)

	if err := s.downloadFile(ctx, appUpdatePath, appUpdate.url, "app", logger); err != nil {
		return s.componentUpdateError("Error downloading app update", err, logger)
	}

	downloadFinished := time.Now()
	appUpdate.downloadFinishedAt = downloadFinished
	appUpdate.downloadProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	if err := s.verifyFile(
		appUpdatePath,
		appUpdate.hash,
		&appUpdate.verificationProgress,
		logger,
	); err != nil {
		return s.componentUpdateError("Error verifying app update hash", err, logger)
	}
	verifyFinished := time.Now()
	appUpdate.verifiedAt = verifyFinished
	appUpdate.verificationProgress = 1
	appUpdate.updatedAt = verifyFinished
	appUpdate.updateProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	logger.Info().Msg("App update downloaded")

	s.rebootNeeded = true
	return nil
}
