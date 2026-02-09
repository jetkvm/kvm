package ota

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

const (
	systemUpdatePath = "/userdata/jetkvm/update_system.tar"
)

// DO NOT call it directly, it's not thread safe
// Mutex is currently held by the caller, e.g. doUpdate
func (s *State) updateSystem(ctx context.Context, systemUpdate *componentUpdateStatus, bypassSignatureCheck bool) error {
	l := s.l.With().Str("path", systemUpdatePath).Logger()

	// Validate signature requirement and download if available
	signature, err := s.downloadComponentSignature(ctx, systemUpdate, "system", &l, bypassSignatureCheck)
	if err != nil {
		return s.componentUpdateError("Error with system signature", err, &l)
	}

	if err := s.downloadFile(ctx, systemUpdatePath, systemUpdate.url, "system"); err != nil {
		return s.componentUpdateError("Error downloading system update", err, &l)
	}

	downloadFinished := time.Now()
	systemUpdate.downloadFinishedAt = downloadFinished
	systemUpdate.downloadProgress = 1
	s.triggerComponentUpdateState("system", systemUpdate)

	if err := s.verifyFile(
		ctx,
		systemUpdatePath,
		systemUpdate.hash,
		signature,
		&systemUpdate.verificationProgress,
	); err != nil {
		return s.componentUpdateError("Error verifying system update", err, &l)
	}
	verifyFinished := time.Now()
	systemUpdate.verifiedAt = verifyFinished
	systemUpdate.verificationProgress = 1
	systemUpdate.updatedAt = verifyFinished
	systemUpdate.updateProgress = 1
	s.triggerComponentUpdateState("system", systemUpdate)

	l.Info().Msg("System update downloaded")

	l.Info().Msg("Starting rk_ota command")

	cmd := exec.Command("rk_ota", "--misc=update", "--tar_path=/userdata/jetkvm/update_system.tar", "--save_dir=/userdata/jetkvm/ota_save", "--partition=all")
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Start(); err != nil {
		return s.componentUpdateError("Error starting rk_ota command", err, &l)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(1800 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if systemUpdate.updateProgress >= 0.99 {
					return
				}
				systemUpdate.updateProgress += 0.01
				if systemUpdate.updateProgress > 0.99 {
					systemUpdate.updateProgress = 0.99
				}
				s.triggerComponentUpdateState("system", systemUpdate)
			case <-ctx.Done():
				return
			}
		}
	}()

	err = cmd.Wait()
	cancel()
	rkLogger := s.l.With().
		Str("output", b.String()).
		Int("exitCode", cmd.ProcessState.ExitCode()).Logger()
	if err != nil {
		return s.componentUpdateError("Error executing rk_ota command", err, &rkLogger)
	}
	rkLogger.Info().Msg("rk_ota success")

	s.rebootNeeded = true
	systemUpdate.updateProgress = 1
	systemUpdate.updatedAt = verifyFinished
	s.triggerComponentUpdateState("system", systemUpdate)

	return nil
}

func (s *State) confirmCurrentSystem() {
	output, err := exec.Command("rk_ota", "--misc=now").CombinedOutput()
	if err != nil {
		s.l.Warn().Str("output", string(output)).Msg("failed to set current partition in A/B setup")
	}
	s.l.Trace().Str("output", string(output)).Msg("current partition in A/B setup set")
}
