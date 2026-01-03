//go:build linux

package tailscale

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// isPathSafe checks if the target path is safely within the base directory.
// Returns an error if the path escapes the base directory (path traversal attack).
func isPathSafe(basePath, targetPath string) error {
	cleanBase := filepath.Clean(basePath) + string(filepath.Separator)
	cleanTarget := filepath.Clean(targetPath)

	// Target must be within base directory (or be the base directory itself)
	if !strings.HasPrefix(cleanTarget, cleanBase) && cleanTarget != filepath.Clean(basePath) {
		return fmt.Errorf("path escapes base directory: %s", targetPath)
	}
	return nil
}

// Downloader handles downloading and installing Tailscale binaries.
type Downloader struct {
	version    string
	httpClient HTTPClient
}

// DefaultHTTPClient implements HTTPClient using net/http.
type DefaultHTTPClient struct {
	client *http.Client
}

func NewDefaultHTTPClient() *DefaultHTTPClient {
	return &DefaultHTTPClient{client: &http.Client{
		Timeout: 10 * time.Minute,
	}}
}

func (c *DefaultHTTPClient) Get(url string) ([]byte, error) {
	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *DefaultHTTPClient) Download(url string, dest string, progress meshvpn.ProgressFunc) error {
	resp, err := c.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	written := int64(0)

	reader := &progressReader{
		reader: resp.Body,
		onProgress: func(n int64) {
			written += n
			if progress != nil && totalSize > 0 {
				progress(float64(written) / float64(totalSize))
			}
		},
	}

	_, err = io.Copy(out, reader)
	return err
}

type progressReader struct {
	reader     io.Reader
	onProgress func(n int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if pr.onProgress != nil && n > 0 {
		pr.onProgress(int64(n))
	}
	return n, err
}

func (d *Downloader) getPackageURL() string {
	return fmt.Sprintf("%s/tailscale_%s_arm.tgz", BaseDownloadURL, d.version)
}

func (d *Downloader) getChecksumURL() string {
	return fmt.Sprintf("%s/tailscale_%s_arm.tgz.sha256", BaseDownloadURL, d.version)
}

// Install downloads, verifies, and installs Tailscale.
// Progress: 0-10% checksum, 10-70% download, 70-80% verify, 80-95% extract, 95-100% configure.
func (d *Downloader) Install(ctx context.Context, progress meshvpn.ProgressFunc) error {
	logger.Info().Str("version", d.version).Msg("starting Tailscale installation")

	reportProgress := func(stage int, stageProgress float64) {
		if progress == nil {
			return
		}
		var overall float64
		switch stage {
		case 0:
			overall = stageProgress * 0.1
		case 1:
			overall = 0.1 + stageProgress*0.6
		case 2:
			overall = 0.7 + stageProgress*0.1
		case 3:
			overall = 0.8 + stageProgress*0.15
		case 4:
			overall = 0.95 + stageProgress*0.05
		}
		progress(overall)
	}

	logger.Debug().Msg("downloading checksum")
	reportProgress(0, 0)

	expectedHash, err := d.downloadChecksum(ctx)
	if err != nil {
		return fmt.Errorf("failed to download checksum: %w", err)
	}
	reportProgress(0, 1.0)

	logger.Debug().Str("hash", expectedHash).Msg("got expected hash")

	logger.Debug().Msg("downloading package")
	reportProgress(1, 0)

	tmpFile, err := os.CreateTemp("", "tailscale-*.tgz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	httpClient := d.httpClient
	if httpClient == nil {
		httpClient = NewDefaultHTTPClient()
	}

	err = httpClient.Download(d.getPackageURL(), tmpPath, func(p float64) {
		reportProgress(1, p)
	})
	if err != nil {
		return fmt.Errorf("failed to download package: %w", err)
	}
	reportProgress(1, 1.0)

	logger.Debug().Msg("verifying checksum")
	reportProgress(2, 0)

	actualHash, err := d.hashFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to hash file: %w", err)
	}

	if actualHash != expectedHash {
		return fmt.Errorf("%w: expected %s, got %s", meshvpn.ErrVerificationFailed, expectedHash, actualHash)
	}
	reportProgress(2, 1.0)

	logger.Debug().Msg("checksum verified")

	logger.Debug().Msg("extracting package")
	reportProgress(3, 0)

	if err := d.extract(tmpPath); err != nil {
		return fmt.Errorf("failed to extract package: %w", err)
	}
	reportProgress(3, 1.0)

	logger.Debug().Msg("configuring")
	reportProgress(4, 0)

	if err := d.configure(ctx); err != nil {
		return fmt.Errorf("failed to configure: %w", err)
	}
	reportProgress(4, 1.0)

	logger.Info().Str("version", d.version).Msg("Tailscale installation complete")

	return nil
}

func (d *Downloader) downloadChecksum(ctx context.Context) (string, error) {
	httpClient := d.httpClient
	if httpClient == nil {
		httpClient = NewDefaultHTTPClient()
	}

	data, err := httpClient.Get(d.getChecksumURL())
	if err != nil {
		return "", err
	}

	hash := strings.TrimSpace(string(data))
	parts := strings.Fields(hash)
	if len(parts) > 0 {
		hash = parts[0]
	}

	return hash, nil
}

func (d *Downloader) hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (d *Downloader) extract(tarballPath string) error {
	if err := os.MkdirAll(InstallBasePath, 0755); err != nil {
		return err
	}

	stateDir := filepath.Dir(StatePath)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}

	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	prefix := fmt.Sprintf("tailscale_%s_arm/", d.version)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := strings.TrimPrefix(header.Name, prefix)

		if name == "" {
			continue
		}

		// Security: reject symlinks and hard links to prevent symlink attacks
		switch header.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive contains forbidden link type: %s", name)
		}

		target := filepath.Join(InstallBasePath, name)

		// Security: prevent path traversal attacks by ensuring target stays within InstallBasePath
		if err := isPathSafe(InstallBasePath, target); err != nil {
			return fmt.Errorf("archive contains path traversal attempt: %s", name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func (d *Downloader) configure(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, TailscalePath, "configure", "jetkvm")
	cmd.Dir = InstallBasePath

	output, err := cmd.CombinedOutput()
	if err != nil {
		// The "tailscale configure jetkvm" command is a JetKVM-specific extension
		// that may not exist in standard Tailscale releases. Check if this is the
		// expected "command not found" case vs an unexpected error.
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 with "unknown command" typically means the command doesn't exist
			if strings.Contains(string(output), "unknown command") {
				logger.Debug().Msg("tailscale configure jetkvm not available in this version (expected)")
			} else {
				// Unexpected error - log but don't fail installation
				logger.Warn().Err(err).Str("output", string(output)).Int("exitCode", exitErr.ExitCode()).
					Msg("tailscale configure jetkvm failed with unexpected error")
			}
		} else {
			// Non-exit error (e.g., context cancelled, binary not found)
			return fmt.Errorf("failed to run tailscale configure: %w", err)
		}
	}

	// Ensure all extracted files are flushed to disk before proceeding.
	// This is important on systems with aggressive write caching.
	if err := exec.CommandContext(ctx, "sync").Run(); err != nil {
		logger.Warn().Err(err).Msg("filesystem sync failed after configuration")
	}

	return nil
}
