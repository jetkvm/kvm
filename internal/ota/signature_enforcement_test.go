package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldBypassSignatureCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		version      string
		custom       bool
		expectBypass bool
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
			name:         "prerelease version bypasses",
			version:      "1.2.3-dev.1",
			custom:       false,
			expectBypass: true,
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
			assert.Equal(t, tt.expectBypass, shouldBypassSignatureCheck(tt.version, tt.custom))
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
