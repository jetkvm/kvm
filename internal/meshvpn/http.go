package meshvpn

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

var httpLogger = logging.GetSubsystemLogger("meshvpn.http")

// HTTPClient interface for HTTP operations used by providers.
type HTTPClient interface {
	Get(url string) ([]byte, error)
	Download(url string, dest string, progress ProgressFunc) error
}

// DefaultHTTPClient implements HTTPClient using net/http.
type DefaultHTTPClient struct {
	client *http.Client
}

// NewDefaultHTTPClient creates a new HTTP client with appropriate timeouts.
// Uses http.DefaultTransport as the base to inherit root CA certificates
// that were loaded by rootcerts.UpdateDefaultTransport() at startup.
// The client has:
// - 30 second connection timeout
// - 60 second TLS handshake timeout
// - 10 minute overall request timeout
func NewDefaultHTTPClient() *DefaultHTTPClient {
	// Clone the default transport to inherit TLS config (including root CAs)
	// but with our custom timeout settings
	var transport *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = dt.Clone()
		transport.DialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
		transport.TLSHandshakeTimeout = 60 * time.Second
		transport.ResponseHeaderTimeout = 60 * time.Second
		transport.IdleConnTimeout = 90 * time.Second
	} else {
		// Fallback if DefaultTransport is not *http.Transport
		transport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   60 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}
		httpLogger.Warn().Msg("could not clone DefaultTransport, root CAs may not be available")
	}

	return &DefaultHTTPClient{client: &http.Client{
		Timeout:   10 * time.Minute,
		Transport: transport,
	}}
}

func (c *DefaultHTTPClient) Get(url string) ([]byte, error) {
	httpLogger.Debug().Str("url", url).Msg("GET request starting")

	resp, err := c.client.Get(url)
	if err != nil {
		httpLogger.Warn().Err(err).Str("url", url).Msg("GET request failed")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpLogger.Warn().Str("url", url).Int("status", resp.StatusCode).Msg("GET request returned non-OK status")
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		httpLogger.Warn().Err(err).Str("url", url).Msg("failed to read response body")
		return nil, err
	}

	httpLogger.Debug().Str("url", url).Int("bytes", len(data)).Msg("GET request completed")
	return data, nil
}

func (c *DefaultHTTPClient) Download(url string, dest string, progress ProgressFunc) error {
	httpLogger.Info().Str("url", url).Str("dest", dest).Msg("download starting")

	resp, err := c.client.Get(url)
	if err != nil {
		httpLogger.Error().Err(err).Str("url", url).Msg("download request failed")
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpLogger.Error().Str("url", url).Int("status", resp.StatusCode).Msg("download returned non-OK status")
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		httpLogger.Error().Err(err).Str("dest", dest).Msg("failed to create destination file")
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	written := int64(0)
	lastProgress := time.Now()
	lastWritten := int64(0)
	stallTimeout := 60 * time.Second // Consider download stalled if no progress for 60 seconds

	httpLogger.Info().Int64("totalSize", totalSize).Msg("download response received, starting transfer")

	// Create a context that we can cancel if download stalls
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Progress monitoring goroutine
	stallCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentWritten := written
				if currentWritten > lastWritten {
					lastWritten = currentWritten
					lastProgress = time.Now()
					pct := float64(0)
					if totalSize > 0 {
						pct = float64(currentWritten) / float64(totalSize) * 100
					}
					httpLogger.Debug().
						Int64("written", currentWritten).
						Int64("total", totalSize).
						Float64("pct", pct).
						Msg("download progress")
				} else if time.Since(lastProgress) > stallTimeout {
					httpLogger.Error().
						Int64("written", currentWritten).
						Int64("total", totalSize).
						Dur("stallDuration", time.Since(lastProgress)).
						Msg("download stalled, aborting")
					close(stallCh)
					return
				}
			}
		}
	}()

	reader := &progressReader{
		reader: resp.Body,
		onProgress: func(n int64) {
			written += n
			if progress != nil && totalSize > 0 {
				progress(float64(written) / float64(totalSize))
			}
		},
		stallCh: stallCh,
	}

	_, err = io.Copy(out, reader)
	cancel() // Stop the monitoring goroutine

	if err != nil {
		httpLogger.Error().Err(err).Int64("written", written).Int64("total", totalSize).Msg("download failed")
		return fmt.Errorf("download failed after %d bytes: %w", written, err)
	}

	httpLogger.Info().Int64("written", written).Str("dest", dest).Msg("download completed successfully")
	return nil
}

type progressReader struct {
	reader     io.Reader
	onProgress func(n int64)
	stallCh    <-chan struct{}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	// Check if download was aborted due to stall
	select {
	case <-pr.stallCh:
		return 0, fmt.Errorf("download stalled")
	default:
	}

	n, err := pr.reader.Read(p)
	if pr.onProgress != nil && n > 0 {
		pr.onProgress(int64(n))
	}
	return n, err
}
