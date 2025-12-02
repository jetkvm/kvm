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
func (s *State) updateSystem(ctx context.Context, systemUpdate *componentUpdateStatus) error {
	loggingContext := s.loggingContext.Str("component", "system").Str("path", systemUpdatePath)

	downloadStarted := time.Now()
	if err := s.downloadFile(ctx, systemUpdatePath, systemUpdate.url, "system"); err != nil {
		return s.componentUpdateError("Error downloading system update", err, loggingContext)
	}
	downloadFinished := time.Now()
	loggingContext.Dur("download_time", downloadFinished.Sub(downloadStarted)).Info().Msg("update downloaded")

	systemUpdate.downloadFinishedAt = downloadFinished
	systemUpdate.downloadProgress = 1
	systemUpdate.updateProgress = 0.25
	s.triggerComponentUpdateState("system", systemUpdate)

	verifyStarted := time.Now()
	if err := s.verifyFile(
		systemUpdatePath,
		systemUpdate.hash,
		&systemUpdate.verificationProgress,
	); err != nil {
		return s.componentUpdateError("Error verifying system update hash", err, loggingContext)
	}
	verifyFinished := time.Now()
	loggingContext.Dur("verification_time", verifyFinished.Sub(verifyStarted)).Info().Msg("update verified")

	systemUpdate.verifiedAt = verifyFinished
	systemUpdate.verificationProgress = 1
	systemUpdate.updatedAt = verifyFinished // TODO, this seems wrong here
	systemUpdate.updateProgress = 0.5
	s.triggerComponentUpdateState("system", systemUpdate)

	loggingContext.Info().Msg("Starting rk_ota command")

	upgradeStarted := time.Now()
	cmd := exec.Command("rk_ota", "--misc=update", "--tar_path=/userdata/jetkvm/update_system.tar", "--save_dir=/userdata/jetkvm/ota_save", "--partition=all")
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Start(); err != nil {
		return s.componentUpdateError("Error starting rk_ota command", err, loggingContext)
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

	err := cmd.Wait()
	cancel()

	upgradeFinished := time.Now()
	loggingContext.Dur("upgrade_time", upgradeFinished.Sub(upgradeStarted)).Info().Msg("upgrade completed")

	loggingContext = loggingContext.Str("output", b.String()).Int("exitCode", cmd.ProcessState.ExitCode())
	if err != nil {
		return s.componentUpdateError("Error executing rk_ota command", err, loggingContext)
	}
	loggingContext.Info().Msg("rk_ota success")

	s.rebootNeeded = true
	systemUpdate.updateProgress = 1
	systemUpdate.updatedAt = verifyFinished
	s.triggerComponentUpdateState("system", systemUpdate)

	return nil
}

func (s *State) confirmCurrentSystem() {
	output, err := exec.Command("rk_ota", "--misc=now").CombinedOutput()
	if err != nil {
		s.loggingContext.Str("output", string(output)).Err(err).Warn().Msg("failed to set current partition in A/B setup")
	}
	s.loggingContext.Str("output", string(output)).Trace().Msg("current partition in A/B setup set")
}
