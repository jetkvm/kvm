package meshvpn

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// HTTPClient interface for HTTP operations used by providers.
type HTTPClient interface {
	Get(url string) ([]byte, error)
	Download(url string, dest string, progress ProgressFunc) error
}

// DefaultHTTPClient implements HTTPClient using net/http.
type DefaultHTTPClient struct {
	client *http.Client
}

// NewDefaultHTTPClient creates a new HTTP client with a 10-minute timeout.
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

func (c *DefaultHTTPClient) Download(url string, dest string, progress ProgressFunc) error {
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
