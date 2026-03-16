package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

func syncFilesystem() error {
	// Flush filesystem buffers to ensure all data is written to disk
	if err := exec.Command("sync").Run(); err != nil {
		return fmt.Errorf("error flushing filesystem buffers: %w", err)
	}

	// Clear the filesystem caches to force a read from disk
	if err := os.WriteFile("/proc/sys/vm/drop_caches", []byte("1"), 0644); err != nil {
		return fmt.Errorf("error clearing filesystem caches: %w", err)
	}

	return nil
}

func (s *State) downloadFile(ctx context.Context, path string, url string, component string) error {
	logger := s.l.With().
		Str("path", path).
		Str("url", url).
		Str("downloadComponent", component).
		Logger()
	t := time.Now()
	traceLogger := func() *zerolog.Event {
		return logger.Trace().Dur("duration", time.Since(t))
	}
	traceLogger().Msg("downloading file")

	componentUpdate, ok := s.componentUpdateStatuses[component]
	if !ok {
		return fmt.Errorf("component %s not found", component)
	}

	downloadProgress := componentUpdate.downloadProgress

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error removing existing file: %w", err)
	}

	unverifiedPath := path + ".unverified"
	if err := os.Remove(unverifiedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error removing existing unverified file: %w", err)
	}

	traceLogger().Msg("creating unverified file")
	file, err := os.Create(unverifiedPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	defer file.Close()

	traceLogger().Msg("creating request")
	req, err := s.newHTTPRequestWithTrace(ctx, "GET", url, nil, traceLogger)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	client := s.client()
	traceLogger().Msg("starting download")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error downloading file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		return fmt.Errorf("invalid content length")
	}

	var written int64
	buf := make([]byte, 32*1024)
	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			nw, ew := file.Write(buf[0:nr])
			if nw < nr {
				return fmt.Errorf("short write: %d < %d", nw, nr)
			}
			written += int64(nw)
			if ew != nil {
				return fmt.Errorf("error writing to file: %w", ew)
			}
			progress := float32(written) / float32(totalSize)
			if progress-downloadProgress >= 0.01 {
				componentUpdate.downloadProgress = progress
				s.triggerComponentUpdateState(component, &componentUpdate)
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return fmt.Errorf("error reading response body: %w", er)
		}
	}

	traceLogger().Msg("download finished")
	file.Close()

	traceLogger().Msg("syncing filesystem")
	if err := syncFilesystem(); err != nil {
		return fmt.Errorf("error syncing filesystem: %w", err)
	}

	return nil
}

// downloadComponentSignature checks if a signature is required and downloads it if available.
// Returns the signature bytes (nil if not required/available) or an error if required but missing.
func (s *State) downloadComponentSignature(
	ctx context.Context,
	update *componentUpdateStatus,
	componentName string,
	l *zerolog.Logger,
	bypassSignatureCheck bool,
) ([]byte, error) {
	if bypassSignatureCheck {
		l.Warn().
			Str("component", componentName).
			Str("localVersion", update.localVersion).
			Str("targetVersion", update.version).
			Msg("bypassing GPG signature check for OTA update")
		return nil, nil
	}

	// Signature is required by default.
	if update.sigUrl == "" {
		return nil, fmt.Errorf("version %s requires GPG signature but API returned no signature URL", update.version)
	}

	l.Debug().Str("sigUrl", update.sigUrl).Msgf("downloading %s signature", componentName)
	return s.downloadSignature(ctx, update.sigUrl)
}

// downloadSignature downloads a detached GPG signature file from the given URL.
// Returns the signature bytes or an error.
func (s *State) downloadSignature(ctx context.Context, sigURL string) ([]byte, error) {
	l := s.l.With().Str("sigURL", sigURL).Logger()
	l.Debug().Msg("downloading signature file")

	req, err := http.NewRequestWithContext(ctx, "GET", sigURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating signature request: %w", err)
	}

	client := s.client()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error downloading signature: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signature download failed with status %d", resp.StatusCode)
	}

	signature, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading signature: %w", err)
	}

	if len(signature) == 0 {
		return nil, fmt.Errorf("signature file is empty")
	}

	l.Debug().Int("signatureBytes", len(signature)).Msg("signature downloaded")
	return signature, nil
}

// verifyFile verifies the SHA256 hash of the downloaded file and optionally verifies
// the GPG signature if signature bytes are provided.
func (s *State) verifyFile(ctx context.Context, path string, expectedHash string, signature []byte, verifyProgress *float32) error {
	l := s.l.With().Str("path", path).Logger()

	unverifiedPath := path + ".unverified"
	fileToHash, err := os.Open(unverifiedPath)
	if err != nil {
		return fmt.Errorf("error opening file for hashing: %w", err)
	}
	defer fileToHash.Close()

	hash := sha256.New()
	fileInfo, err := fileToHash.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}
	totalSize := fileInfo.Size()

	buf := make([]byte, 32*1024)
	verified := int64(0)

	for {
		nr, er := fileToHash.Read(buf)
		if nr > 0 {
			nw, ew := hash.Write(buf[0:nr])
			if nw < nr {
				return fmt.Errorf("short write: %d < %d", nw, nr)
			}
			verified += int64(nw)
			if ew != nil {
				return fmt.Errorf("error writing to hash: %w", ew)
			}
			progress := float32(verified) / float32(totalSize)
			if progress-*verifyProgress >= 0.01 {
				*verifyProgress = progress
				s.triggerStateUpdate()
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return fmt.Errorf("error reading file: %w", er)
		}
	}

	hashSum := hash.Sum(nil)
	l.Info().Str("hash", hex.EncodeToString(hashSum)).Msg("SHA256 hash of")

	if hex.EncodeToString(hashSum) != expectedHash {
		return fmt.Errorf("hash mismatch: %x != %s", hashSum, expectedHash)
	}

	// Verify GPG signature if provided
	if len(signature) > 0 {
		l.Info().Msg("verifying GPG signature")
		if err := s.gpgVerifier.VerifySignatureFromFile(ctx, signature, unverifiedPath); err != nil {
			return fmt.Errorf("GPG signature verification failed: %w", err)
		}
		l.Info().Msg("GPG signature verified successfully")
	}

	if err := os.Rename(unverifiedPath, path); err != nil {
		return fmt.Errorf("error renaming file: %w", err)
	}

	if err := os.Chmod(path, 0755); err != nil {
		return fmt.Errorf("error making file executable: %w", err)
	}

	return nil
}

// shouldBypassSignatureCheck determines whether GPG signature verification can
// be skipped for this update. Bypass is allowed in two cases:
//
//  1. The user explicitly requested a specific version (customVersionUpdate).
//     This requires local user action on the device, so it can't be triggered
//     by a compromised server.
//
//  2. The remote version is a pre-release AND the device has opted into the
//     pre-release channel (includePreRelease). We check the local opt-in flag
//     so that a compromised server can't push an unsigned pre-release version
//     string to devices that never subscribed to the dev channel.
func shouldBypassSignatureCheck(version string, customVersionUpdate bool, includePreRelease bool) bool {
	if customVersionUpdate {
		return true
	}

	remoteVersion, err := semver.NewVersion(version)
	if err != nil {
		return false
	}

	isPrerelease := remoteVersion.Prerelease() != ""
	return isPrerelease && includePreRelease
}
