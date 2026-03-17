package ota

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

// buildArchive creates a tar.gz in memory from a map of filename→content.
func buildArchive(t *testing.T, files map[string][]byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		err := tw.WriteHeader(&tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		})
		require.NoError(t, err)
		_, err = tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return &buf
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func testLogger() *zerolog.Logger {
	l := zerolog.New(io.Discard)
	return &l
}

func TestExtractOfflineArchive_ValidApp(t *testing.T) {
	binary := []byte("fake-app-binary")
	hash := sha256hex(binary)
	sig := []byte("fake-signature-bytes")

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash + "  jetkvm_app\n"),
		"jetkvm_app.sig":    sig,
	})

	destDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	require.NoError(t, err)

	assert.Equal(t, "app", bundle.Component)
	assert.Equal(t, hash, bundle.ExpectedHash)
	assert.Equal(t, sig, bundle.Signature)
	assert.Equal(t, filepath.Join(destDir, "jetkvm_app"), bundle.BinaryPath)

	// binary should be on disk
	content, err := os.ReadFile(bundle.BinaryPath)
	require.NoError(t, err)
	assert.Equal(t, binary, content)
}

func TestExtractOfflineArchive_ValidSystem(t *testing.T) {
	binary := []byte("fake-system-tar")
	hash := sha256hex(binary)
	sig := []byte("fake-sig")

	archive := buildArchive(t, map[string][]byte{
		"update_system.tar":        binary,
		"update_system.tar.sha256": []byte(hash),
		"update_system.tar.sig":    sig,
	})

	destDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, destDir, "system", testLogger())
	require.NoError(t, err)

	assert.Equal(t, "system", bundle.Component)
	assert.Equal(t, hash, bundle.ExpectedHash)
}

func TestExtractOfflineArchive_HashOnly(t *testing.T) {
	binary := []byte("binary")
	hash := sha256hex(binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required signature file")
}

func TestExtractOfflineArchive_MissingHash(t *testing.T) {
	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":     []byte("binary"),
		"jetkvm_app.sig": []byte("sig"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required hash file")
}

func TestExtractOfflineArchive_MissingBinary(t *testing.T) {
	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app.sha256": []byte("abc123"),
		"jetkvm_app.sig":    []byte("sig"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required binary")
}

func TestExtractOfflineArchive_UnexpectedFile(t *testing.T) {
	binary := []byte("binary")
	hash := sha256hex(binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
		"jetkvm_app.sig":    []byte("sig"),
		"malicious.sh":      []byte("#!/bin/bash\nrm -rf /"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.Error(t, err) // either "unexpected file" or "more than 3 files"
}

func TestExtractOfflineArchive_PathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	_ = tw.WriteHeader(&tar.Header{
		Name: "../../etc/passwd",
		Size: 5,
		Mode: 0644,
	})
	_, _ = tw.Write([]byte("pwned"))
	_ = tw.Close()
	_ = gw.Close()

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(&buf, destDir, "app", testLogger())
	// Will fail as unexpected file since basename won't match expected names
	assert.Error(t, err)
}

func TestExtractOfflineArchive_UnknownComponent(t *testing.T) {
	archive := buildArchive(t, map[string][]byte{})
	_, err := ExtractOfflineArchive(archive, t.TempDir(), "unknown", testLogger())
	assert.ErrorContains(t, err, "unknown component")
}

func TestExtractOfflineArchive_CorruptGzip(t *testing.T) {
	_, err := ExtractOfflineArchive(bytes.NewReader([]byte("not-gzip")), t.TempDir(), "app", testLogger())
	assert.ErrorContains(t, err, "gzip")
}

func TestExtractOfflineArchive_NestedDirectory(t *testing.T) {
	// Archives created with `tar czf` often wrap files in a directory.
	// The extractor should strip the leading directory and match by basename.
	binary := []byte("app-binary")
	hash := sha256hex(binary)
	sig := []byte("sig-bytes")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range map[string][]byte{
		"jetkvm_app_offline_update/jetkvm_app":        binary,
		"jetkvm_app_offline_update/jetkvm_app.sha256": []byte(hash + "  jetkvm_app\n"),
		"jetkvm_app_offline_update/jetkvm_app.sig":    sig,
	} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(content)), Mode: 0644})
		_, _ = tw.Write(content)
	}
	_ = tw.Close()
	_ = gw.Close()

	destDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(&buf, destDir, "app", testLogger())
	require.NoError(t, err)
	assert.Equal(t, hash, bundle.ExpectedHash)
	assert.Equal(t, sig, bundle.Signature)
}

func TestHashFile(t *testing.T) {
	content := []byte("hello world")
	expected := sha256hex(content)

	path := filepath.Join(t.TempDir(), "test")
	require.NoError(t, os.WriteFile(path, content, 0644))

	got, err := hashFile(path)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestIsKeyFetchError(t *testing.T) {
	tests := []struct {
		err    string
		expect bool
	}{
		{"all keyservers failed: [err1, err2]", true},
		{"failed to fetch public key: connection refused", true},
		{"key fetch cancelled: context deadline exceeded", true},
		{"signature verification failed: openpgp: invalid signature", false},
		{"hash mismatch: abc != def", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expect, isKeyFetchError(tt.err), "input: %s", tt.err)
	}
}

// --- VerifyOfflineBundle tests ---

// newSigningTestFixture generates a GPG key pair and returns:
//   - a GPGVerifier wired to a mock keyserver that serves the public key
//   - the private entity (for producing signatures)
//   - a cleanup function (restores global keyservers)
func newSigningTestFixture(t *testing.T) (*GPGVerifier, *openpgp.Entity) {
	t.Helper()

	entity, err := openpgp.NewEntity("Offline Test", "", "offline@test.local", nil)
	require.NoError(t, err)

	// Armour the public key
	var pubBuf bytes.Buffer
	w, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())

	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: pubBuf.Bytes(), callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, pubBuf.Bytes())

	return v, entity
}

// signData produces a detached GPG signature over data using entity's private key.
func signData(t *testing.T, entity *openpgp.Entity, data []byte) []byte {
	t.Helper()
	var sigBuf bytes.Buffer
	err := openpgp.DetachSign(&sigBuf, entity, bytes.NewReader(data), nil)
	require.NoError(t, err)
	return sigBuf.Bytes()
}

// writeBundle writes a binary to disk and returns an OfflineBundle ready for verification.
func writeBundle(t *testing.T, binary []byte, sig []byte) *OfflineBundle {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jetkvm_app")
	require.NoError(t, os.WriteFile(path, binary, 0644))
	return &OfflineBundle{
		BinaryPath:   path,
		ExpectedHash: sha256hex(binary),
		Signature:    sig,
		Component:    "app",
	}
}

func TestVerifyOfflineBundle_ValidSignature(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("valid-app-binary-content")
	sig := signData(t, entity, binary)
	bundle := writeBundle(t, binary, sig)

	result, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK, "hash should pass")
	assert.True(t, result.SignatureOK, "signature should pass")
	assert.False(t, result.KeyFetchFailed, "key fetch should succeed")
	assert.Empty(t, result.SignatureError)
}

func TestVerifyOfflineBundle_HashMismatch(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("real-binary")
	sig := signData(t, entity, binary)
	bundle := writeBundle(t, binary, sig)

	// Corrupt the expected hash so it won't match the file on disk
	bundle.ExpectedHash = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestVerifyOfflineBundle_InvalidSignature(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("the-real-binary")
	differentContent := []byte("tampered-binary")

	// Sign the tampered content, but the bundle points at the real binary.
	// This means the signature won't match the file being verified.
	sig := signData(t, entity, differentContent)
	bundle := writeBundle(t, binary, sig)

	_, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

func TestVerifyOfflineBundle_WrongKey(t *testing.T) {
	// Verifier is wired to key A, but the binary is signed with key B.
	gpgVerifier, _ := newSigningTestFixture(t)

	// Generate a completely different key pair for signing
	otherEntity, err := openpgp.NewEntity("Attacker", "", "evil@attacker.com", nil)
	require.NoError(t, err)

	binary := []byte("innocent-looking-binary")
	sig := signData(t, otherEntity, binary)
	bundle := writeBundle(t, binary, sig)

	_, err = VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

func TestVerifyOfflineBundle_EmptySignature(t *testing.T) {
	gpgVerifier, _ := newSigningTestFixture(t)

	binary := []byte("unsigned-binary")
	bundle := writeBundle(t, binary, nil)
	bundle.Signature = nil

	_, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is required")
}

func TestVerifyOfflineBundle_KeyFetchFailure(t *testing.T) {
	// Simulate an air-gapped device: all keyserver requests fail.
	callCount := &atomic.Int32{}
	mock := &failingHTTPClient{callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	// rootKeyFP doesn't matter since we'll never get a key to compare it against

	binary := []byte("offline-binary")
	sig := []byte("some-signature-bytes") // content irrelevant; key fetch fails first
	bundle := writeBundle(t, binary, sig)

	result, err := VerifyOfflineBundle(context.Background(), bundle, v, testLogger())
	require.NoError(t, err, "key fetch failure should not be a hard error")
	assert.True(t, result.HashOK, "hash should still pass")
	assert.False(t, result.SignatureOK, "signature should not be marked OK")
	assert.True(t, result.KeyFetchFailed, "should indicate key fetch failed")
	assert.NotEmpty(t, result.SignatureError)
}

func TestVerifyOfflineBundle_TruncatedSignature(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("binary-with-truncated-sig")
	fullSig := signData(t, entity, binary)

	// Truncate the signature to corrupt it
	truncatedSig := fullSig[:len(fullSig)/2]
	bundle := writeBundle(t, binary, truncatedSig)

	_, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

func TestVerifyOfflineBundle_CorruptedBinary(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	originalBinary := []byte("original-binary-content")
	sig := signData(t, entity, originalBinary)

	// Write the original, get a valid bundle, then overwrite the file
	bundle := writeBundle(t, originalBinary, sig)
	require.NoError(t, os.WriteFile(bundle.BinaryPath, []byte("corrupted-binary"), 0644))

	// Hash will mismatch because the file content no longer matches ExpectedHash
	_, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

// --- ComponentUpdatePath tests ---

func TestComponentUpdatePath_App(t *testing.T) {
	path, err := ComponentUpdatePath("app")
	require.NoError(t, err)
	assert.Equal(t, appUpdatePath, path)
}

func TestComponentUpdatePath_System(t *testing.T) {
	path, err := ComponentUpdatePath("system")
	require.NoError(t, err)
	assert.Equal(t, systemUpdatePath, path)
}

func TestComponentUpdatePath_Unknown(t *testing.T) {
	_, err := ComponentUpdatePath("unknown")
	assert.ErrorContains(t, err, "unknown component")
}

// --- End-to-end pipeline tests ---

// TestEndToEnd_ExtractAndVerify_ValidArchive exercises the full pipeline:
// build a tar.gz with a real GPG signature → extract → verify.
func TestEndToEnd_ExtractAndVerify_ValidArchive(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("end-to-end-test-binary-content-here")
	hash := sha256hex(binary)
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash + "  jetkvm_app\n"),
		"jetkvm_app.sig":    sig,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	result, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK)
	assert.True(t, result.SignatureOK)
	assert.False(t, result.KeyFetchFailed)
}

// TestEndToEnd_ExtractAndVerify_TamperedBinary builds a valid archive then
// overwrites the extracted binary before verification — simulating
// file-level tampering after extraction.
func TestEndToEnd_ExtractAndVerify_TamperedBinary(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("legitimate-binary")
	hash := sha256hex(binary)
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
		"jetkvm_app.sig":    sig,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	// Tamper with extracted binary on disk
	require.NoError(t, os.WriteFile(bundle.BinaryPath, []byte("tampered!"), 0644))

	_, err = VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

// TestEndToEnd_ExtractAndVerify_WrongSignature builds an archive where
// the signature was produced by a different key than the verifier expects.
func TestEndToEnd_ExtractAndVerify_WrongSignature(t *testing.T) {
	gpgVerifier, _ := newSigningTestFixture(t) // verifier expects key A

	attackerEntity, err := openpgp.NewEntity("Attacker", "", "evil@example.com", nil)
	require.NoError(t, err)

	binary := []byte("innocuous-binary")
	hash := sha256hex(binary)
	sig := signData(t, attackerEntity, binary) // signed with key B

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
		"jetkvm_app.sig":    sig,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	_, err = VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

// TestEndToEnd_ExtractAndVerify_HashMismatchInArchive builds an archive
// where the .sha256 file contains the wrong hash for the binary.
func TestEndToEnd_ExtractAndVerify_HashMismatchInArchive(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("real-binary-content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(wrongHash),
		"jetkvm_app.sig":    sig,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)
	// The extraction succeeds — hash mismatch is caught at verification time
	assert.Equal(t, wrongHash, bundle.ExpectedHash)

	_, err = VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

// TestEndToEnd_SystemArchive verifies the full pipeline works for system
// component archives with the different expected file names.
func TestEndToEnd_SystemArchive(t *testing.T) {
	gpgVerifier, entity := newSigningTestFixture(t)

	binary := []byte("system-image-tar-content")
	hash := sha256hex(binary)
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"update_system.tar":        binary,
		"update_system.tar.sha256": []byte(hash),
		"update_system.tar.sig":    sig,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "system", testLogger())
	require.NoError(t, err)
	assert.Equal(t, "system", bundle.Component)

	result, err := VerifyOfflineBundle(context.Background(), bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK)
	assert.True(t, result.SignatureOK)
}
