//go:build linux

package zerotier

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

// Downloader handles downloading and installing ZeroTier binaries.
type Downloader struct {
	version    string
	httpClient HTTPClient
}

// HTTPClient interface for HTTP operations
type HTTPClient interface {
	Get(url string) ([]byte, error)
	Download(url string, dest string, progress meshvpn.ProgressFunc) error
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
	// Official ZeroTier Debian package containing static-pie linked binary
	return fmt.Sprintf("%s/%s/dist/debian/bullseye/zerotier-one_%s_armhf.deb", BaseDownloadURL, d.version, d.version)
}

// Install downloads and installs ZeroTier.
// Progress: 0-70% download, 70-90% extract, 90-100% setup.
func (d *Downloader) Install(ctx context.Context, progress meshvpn.ProgressFunc) error {
	logger.Info().Str("version", d.version).Msg("starting ZeroTier installation")

	reportProgress := func(stage int, stageProgress float64) {
		if progress == nil {
			return
		}
		var overall float64
		switch stage {
		case 0: // Download
			overall = stageProgress * 0.7
		case 1: // Extract
			overall = 0.7 + stageProgress*0.2
		case 2: // Setup
			overall = 0.9 + stageProgress*0.1
		}
		progress(overall)
	}

	logger.Debug().Msg("downloading package")
	reportProgress(0, 0)

	httpClient := d.httpClient
	if httpClient == nil {
		httpClient = NewDefaultHTTPClient()
	}

	// Download .deb package
	tmpFile, err := os.CreateTemp("", "zerotier-*.deb")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	err = httpClient.Download(d.getPackageURL(), tmpPath, func(p float64) {
		reportProgress(0, p)
	})
	if err != nil {
		return fmt.Errorf("failed to download package: %w", err)
	}
	reportProgress(0, 1.0)

	logger.Debug().Msg("extracting package")
	reportProgress(1, 0)

	if err := d.extractDeb(tmpPath); err != nil {
		return fmt.Errorf("failed to extract package: %w", err)
	}
	reportProgress(1, 1.0)

	logger.Debug().Msg("setting up")
	reportProgress(2, 0)

	if err := d.setup(); err != nil {
		return fmt.Errorf("failed to setup: %w", err)
	}
	reportProgress(2, 1.0)

	logger.Info().Str("version", d.version).Msg("ZeroTier installation complete")

	return nil
}

// extractDeb extracts the zerotier-one binary from a Debian package.
// The .deb format is an ar archive containing data.tar.xz with the actual files.
func (d *Downloader) extractDeb(debPath string) error {
	if err := os.MkdirAll(InstallBasePath, 0755); err != nil {
		return err
	}

	// Create a temporary directory for extraction
	tmpDir, err := os.MkdirTemp("", "zerotier-extract-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract the .deb using ar (or a shell pipeline if ar is not available)
	// .deb files are ar archives containing: debian-binary, control.tar.*, data.tar.*
	// We need to extract data.tar.xz and then extract usr/sbin/zerotier-one from it

	// Use a shell pipeline to extract: ar extracts data.tar.xz, tar extracts the binary
	// Since the device may not have 'ar', we use a workaround that reads the ar format directly
	// Note: All paths are internally generated (MkdirTemp, CreateTemp, constants) so shell
	// injection risk is minimal, but we quote paths for defense in depth.
	extractScript := fmt.Sprintf(`
		cd '%s'
		# Read the .deb (ar archive) and extract data.tar.xz
		# ar format: "!<arch>\n" header, then entries with 60-byte headers

		offset=8  # Skip "!<arch>\n" magic
		while true; do
			header=$(dd if='%s' bs=1 skip=$offset count=60 2>/dev/null)
			[ -z "$header" ] && break

			filename=$(echo "$header" | cut -c1-16 | tr -d ' ')
			size=$(echo "$header" | cut -c49-58 | tr -d ' ')
			offset=$((offset + 60))

			if echo "$filename" | grep -q "^data.tar"; then
				dd if='%s' bs=1 skip=$offset count=$size 2>/dev/null | xzcat | tar -xf - ./usr/sbin/zerotier-one
				break
			fi

			offset=$((offset + size + (size %% 2)))
		done

		if [ -f usr/sbin/zerotier-one ]; then
			mv usr/sbin/zerotier-one '%s'
			chmod 755 '%s'
			exit 0
		else
			exit 1
		fi
	`, tmpDir, debPath, debPath, ZeroTierOnePath, ZeroTierOnePath)

	cmd := exec.Command("sh", "-c", extractScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract deb: %w (output: %s)", err, string(output))
	}

	// Verify the binary was extracted
	if _, err := os.Stat(ZeroTierOnePath); os.IsNotExist(err) {
		return fmt.Errorf("zerotier-one binary not found after extraction")
	}

	logger.Debug().Str("path", ZeroTierOnePath).Msg("extracted zerotier-one binary from deb")

	return nil
}

func (d *Downloader) setup() error {
	// Create symlink for zerotier-cli (zerotier-one responds to both)
	if err := os.Remove(ZeroTierCliPath); err != nil && !os.IsNotExist(err) {
		logger.Warn().Err(err).Msg("failed to remove existing zerotier-cli symlink")
	}

	if err := os.Symlink(ZeroTierOnePath, ZeroTierCliPath); err != nil {
		logger.Warn().Err(err).Msg("failed to create zerotier-cli symlink, will use zerotier-one directly")
	}

	// Create networks.d directory for network configs
	if err := os.MkdirAll(NetworksDirectory, 0700); err != nil {
		return err
	}

	return nil
}
