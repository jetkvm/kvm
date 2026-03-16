package ota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldBypassSignatureCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		version           string
		custom            bool
		includePreRelease bool
		expectBypass      bool
	}{
		{
			name:         "stable version without custom does not bypass",
			version:      "1.2.3",
			custom:       false,
			expectBypass: false,
		},
		{
			name:         "stable version with custom bypasses",
			version:      "1.2.3",
			custom:       true,
			expectBypass: true,
		},
		{
			name:              "prerelease version with opt-in bypasses",
			version:           "1.2.3-dev.1",
			custom:            false,
			includePreRelease: true,
			expectBypass:      true,
		},
		{
			name:              "prerelease version without opt-in does NOT bypass",
			version:           "1.2.3-dev.1",
			custom:            false,
			includePreRelease: false,
			expectBypass:      false,
		},
		{
			name:         "invalid version does not bypass",
			version:      "not-semver",
			custom:       false,
			expectBypass: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expectBypass, shouldBypassSignatureCheck(tt.version, tt.custom, tt.includePreRelease))
		})
	}
}

func TestDownloadComponentSignature_MissingSignatureFailsForSystem(t *testing.T) {
	callCount := &atomic.Int32{}
	client := &signatureMockClient{
		statusCode: 200,
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

func TestDownloadSignature_FailsWithEmptyResponse(t *testing.T) {
	callCount := &atomic.Int32{}
	client := &signatureMockClient{
		statusCode: 200,
		body:       []byte{},
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
	require.Error(t, err)
	assert.Nil(t, sig)
	assert.ErrorContains(t, err, "signature file is empty")
	assert.Equal(t, int32(1), callCount.Load(), "server should have been called")
}

func TestVerifyFile_FailsWithInvalidSignature(t *testing.T) {
	t.Parallel()

	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	armoredKey := generateTestArmoredKey(t)
	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: armoredKey, callCount: callCount}
	verifier := NewGPGVerifier(&logger, func() HttpClient { return mock })
	verifier.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	s := &State{
		l: &logger,
		onStateUpdate: func(state *RPCState) {
			// no-op
		},
		gpgVerifier: verifier,
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "jetkvm_app.update")
	unverifiedPath := targetPath + ".unverified"

	data := []byte("test update payload")
	require.NoError(t, os.WriteFile(unverifiedPath, data, 0644))

	hash := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(hash[:])
	var verifyProgress float32

	err := s.verifyFile(
		context.Background(),
		targetPath,
		expectedHash,
		[]byte("this-is-not-a-valid-signature"),
		&verifyProgress,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "GPG signature verification failed")
	assert.Equal(t, int32(1), callCount.Load(), "public key should be fetched once")

	// Failed verification must not promote .unverified to final path.
	_, statErr := os.Stat(targetPath)
	assert.Error(t, statErr)
}

func TestVerifyFile_FailsWithHashMismatch(t *testing.T) {
	t.Parallel()

	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	s := &State{
		l:             &logger,
		onStateUpdate: func(state *RPCState) {},
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "jetkvm_app.update")
	unverifiedPath := targetPath + ".unverified"

	data := []byte("real update payload")
	require.NoError(t, os.WriteFile(unverifiedPath, data, 0644))

	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	var verifyProgress float32

	err := s.verifyFile(
		context.Background(),
		targetPath,
		wrongHash,
		nil,
		&verifyProgress,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "hash mismatch")

	_, statErr := os.Stat(targetPath)
	assert.Error(t, statErr, "hash mismatch must not promote .unverified to final path")
}

func TestDownloadSignature_FailsWithNon200Status(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
		{"403 Forbidden", http.StatusForbidden},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callCount := &atomic.Int32{}
			client := &signatureMockClient{
				statusCode: tt.statusCode,
				body:       []byte("error"),
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
			require.Error(t, err)
			assert.Nil(t, sig)
			assert.ErrorContains(t, err, "signature download failed")
			assert.Equal(t, int32(1), callCount.Load())
		})
	}
}

func TestVerifyFile_SucceedsWithValidSignature(t *testing.T) {
	t.Parallel()

	// Use a single entity for both serving the public key and signing the data.
	entity, err := openpgp.NewEntity("Test", "", "test@example.com", nil)
	require.NoError(t, err)

	var pubBuf bytes.Buffer
	w, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())
	armoredKey := pubBuf.Bytes()

	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: armoredKey, callCount: callCount}

	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	verifier := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	verifier.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	s := &State{
		l:             &logger,
		onStateUpdate: func(state *RPCState) {},
		gpgVerifier:   verifier,
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "jetkvm_app.update")
	unverifiedPath := targetPath + ".unverified"

	data := []byte("legitimate update payload")
	require.NoError(t, os.WriteFile(unverifiedPath, data, 0644))

	hash := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(hash[:])

	var sigBuf bytes.Buffer
	require.NoError(t, openpgp.DetachSign(&sigBuf, entity, bytes.NewReader(data), nil))

	var verifyProgress float32
	err = s.verifyFile(
		context.Background(),
		targetPath,
		expectedHash,
		sigBuf.Bytes(),
		&verifyProgress,
	)
	require.NoError(t, err)

	info, statErr := os.Stat(targetPath)
	require.NoError(t, statErr, "verified file should be promoted to final path")
	assert.Equal(t, int64(len(data)), info.Size())

	_, statErr = os.Stat(unverifiedPath)
	assert.Error(t, statErr, ".unverified should be gone after successful rename")
}
