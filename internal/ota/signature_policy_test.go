package ota

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signatureMockClient struct {
	statusCode int
	body       []byte
	callCount  *atomic.Int32
}

func (c *signatureMockClient) Do(req *http.Request) (*http.Response, error) {
	c.callCount.Add(1)
	return &http.Response{
		StatusCode: c.statusCode,
		Body:       io.NopCloser(bytes.NewReader(c.body)),
	}, nil
}

func newSignaturePolicyState(client HttpClient) *State {
	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	return &State{
		l: &logger,
		client: func() HttpClient {
			return client
		},
	}
}

func TestDownloadComponentSignature_BypassSkipsDownload(t *testing.T) {
	callCount := &atomic.Int32{}
	client := &signatureMockClient{
		statusCode: http.StatusOK,
		body:       []byte("sig-data"),
		callCount:  callCount,
	}
	s := newSignaturePolicyState(client)

	sig, err := s.downloadComponentSignature(
		context.Background(),
		&componentUpdateStatus{
			localVersion: "1.0.0",
			version:      "1.0.1",
			sigUrl:       "https://example.com/sig",
		},
		"app",
		s.l,
		true,
	)
	require.NoError(t, err)
	assert.Nil(t, sig)
	assert.Equal(t, int32(0), callCount.Load())
}

func TestDownloadComponentSignature_MissingSignatureFails(t *testing.T) {
	callCount := &atomic.Int32{}
	client := &signatureMockClient{
		statusCode: http.StatusOK,
		body:       []byte("sig-data"),
		callCount:  callCount,
	}
	s := newSignaturePolicyState(client)

	sig, err := s.downloadComponentSignature(
		context.Background(),
		&componentUpdateStatus{
			localVersion: "1.0.0",
			version:      "1.0.1",
			sigUrl:       "",
		},
		"system",
		s.l,
		false,
	)
	require.Error(t, err)
	assert.Nil(t, sig)
	assert.ErrorContains(t, err, "requires GPG signature")
	assert.Equal(t, int32(0), callCount.Load())
}

func TestDownloadComponentSignature_RequiresAndDownloadsSignature(t *testing.T) {
	callCount := &atomic.Int32{}
	client := &signatureMockClient{
		statusCode: http.StatusOK,
		body:       []byte("sig-data"),
		callCount:  callCount,
	}
	s := newSignaturePolicyState(client)

	sig, err := s.downloadComponentSignature(
		context.Background(),
		&componentUpdateStatus{
			localVersion: "1.0.0",
			version:      "1.0.1",
			sigUrl:       "https://example.com/sig",
		},
		"app",
		s.l,
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, []byte("sig-data"), sig)
	assert.Equal(t, int32(1), callCount.Load())
}
