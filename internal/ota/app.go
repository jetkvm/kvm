package ota

import (
	"context"
	"fmt"
	"time"
)

const (
	appUpdatePath = "/userdata/jetkvm/jetkvm_app.update"
)

// DO NOT call it directly, it's not thread safe
// Mutex is currently held by the caller, e.g. doUpdate
func (s *State) updateApp(ctx context.Context, appUpdate *componentUpdateStatus) error {
	l := s.l.With().Str("path", appUpdatePath).Logger()

	// Check early: if signature is required for this version but sigUrl is missing, reject the update
	if s.gpgVerifier.IsSignatureRequired(appUpdate.localVersion, appUpdate.version) && appUpdate.sigUrl == "" {
		return s.componentUpdateError(
			"Update rejected: signature required but not provided",
			fmt.Errorf("version %s requires GPG signature but API returned no signature URL (possible API compromise)", appUpdate.version),
			&l,
		)
	}

	if err := s.downloadFile(ctx, appUpdatePath, appUpdate.url, "app"); err != nil {
		return s.componentUpdateError("Error downloading app update", err, &l)
	}

	downloadFinished := time.Now()
	appUpdate.downloadFinishedAt = downloadFinished
	appUpdate.downloadProgress = 1
	s.triggerComponentUpdateState("app", appUpdate)

	// Download GPG signature
	var signature []byte
	if appUpdate.sigUrl != "" {
		l.Debug().Str("sigUrl", appUpdate.sigUrl).Msg("downloading app signature")
		var err error
		signature, err = s.downloadSignature(ctx, appUpdate.sigUrl)
		if err != nil {
			return s.componentUpdateError("Error downloading app signature", err, &l)
		}
	}

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
