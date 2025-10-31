package ota

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

const (
	appUpdatePath = "/userdata/jetkvm/jetkvm_app.update"
)

func (s *State) componentUpdateError(prefix string, err error, l *zerolog.Logger) error {
	if l == nil {
		l = s.l
	}
	l.Error().Err(err).Msg(prefix)
	s.error = fmt.Sprintf("%s: %v", prefix, err)
	return err
}

func (s *State) updateApp(ctx context.Context, appUpdate *componentUpdateStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	l := s.l.With().Str("path", appUpdatePath).Logger()

	if err := s.downloadFile(ctx, appUpdatePath, appUpdate.url, &appUpdate.downloadProgress); err != nil {
		return s.componentUpdateError("Error downloading app update", err, &l)
	}

	downloadFinished := time.Now()
	appUpdate.downloadFinishedAt = downloadFinished
	appUpdate.downloadProgress = 1
	s.triggerStateUpdate()

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
	s.triggerStateUpdate()

	l.Info().Msg("App update downloaded")

	s.rebootNeeded = true

	return nil
}
