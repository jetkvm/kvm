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

	if _, err := os.Stat(path); err == nil {
		traceLogger().Msg("removing existing file")
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("error removing existing file: %w", err)
		}
	}

	unverifiedPath := path + ".unverified"
	if _, err := os.Stat(unverifiedPath); err == nil {
		traceLogger().Msg("removing existing unverified file")
		if err := os.Remove(unverifiedPath); err != nil {
			return fmt.Errorf("error removing existing unverified file: %w", err)
		}
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
func (s *State) verifyFile(path string, expectedHash string, verifyProgress *float32) error {
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

	if err := os.Rename(unverifiedPath, path); err != nil {
		return fmt.Errorf("error renaming file: %w", err)
	}

	if err := os.Chmod(path, 0755); err != nil {
		return fmt.Errorf("error making file executable: %w", err)
	}

	return nil
}
