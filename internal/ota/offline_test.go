package ota

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// armorPublicKey returns the armored public key bytes for the given entity.
func armorPublicKey(t *testing.T, entity *openpgp.Entity) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// buildAppArchive creates a complete 4-file offline update archive for
// the "app" component using the given binary, signature, and public key.
func buildAppArchive(t *testing.T, binary, sig, pub []byte) *bytes.Buffer {
	t.Helper()
	return buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(sha256hex(binary) + "  jetkvm_app\n"),
		"jetkvm_app.sig":    sig,
		"jetkvm_app.pub":    pub,
	})
}

func TestExtractOfflineArchive_ValidApp(t *testing.T) {
	binary := []byte("fake-app-binary")
	hash := sha256hex(binary)
	sig := []byte("fake-signature-bytes")
	pub := []byte("fake-public-key")

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash + "  jetkvm_app\n"),
		"jetkvm_app.sig":    sig,
		"jetkvm_app.pub":    pub,
	})

	destDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	require.NoError(t, err)

	assert.Equal(t, "app", bundle.Component)
	assert.Equal(t, hash, bundle.ExpectedHash)
	assert.Equal(t, sig, bundle.Signature)
	assert.Equal(t, pub, bundle.PublicKeyData)
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
	pub := []byte("fake-pub")

	archive := buildArchive(t, map[string][]byte{
		"update_system.tar":        binary,
		"update_system.tar.sha256": []byte(hash),
		"update_system.tar.sig":    sig,
		"update_system.tar.pub":    pub,
	})

	destDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, destDir, "system", testLogger())
	require.NoError(t, err)

	assert.Equal(t, "system", bundle.Component)
	assert.Equal(t, hash, bundle.ExpectedHash)
	assert.Equal(t, pub, bundle.PublicKeyData)
}

func TestExtractOfflineArchive_MissingSig(t *testing.T) {
	binary := []byte("binary")
	hash := sha256hex(binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
		"jetkvm_app.pub":    []byte("pub"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required signature file")
}

func TestExtractOfflineArchive_MissingPub(t *testing.T) {
	binary := []byte("binary")
	hash := sha256hex(binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(hash),
		"jetkvm_app.sig":    []byte("sig"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required public key file")
}

func TestExtractOfflineArchive_MissingHash(t *testing.T) {
	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":     []byte("binary"),
		"jetkvm_app.sig": []byte("sig"),
		"jetkvm_app.pub": []byte("pub"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.ErrorContains(t, err, "missing required hash file")
}

func TestExtractOfflineArchive_MissingBinary(t *testing.T) {
	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app.sha256": []byte("abc123"),
		"jetkvm_app.sig":    []byte("sig"),
		"jetkvm_app.pub":    []byte("pub"),
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
		"jetkvm_app.pub":    []byte("pub"),
		"malicious.sh":      []byte("#!/bin/bash\nrm -rf /"),
	})

	destDir := t.TempDir()
	_, err := ExtractOfflineArchive(archive, destDir, "app", testLogger())
	assert.Error(t, err) // either "unexpected file" or "more than 4 files"
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
	binary := []byte("app-binary")
	hash := sha256hex(binary)
	sig := []byte("sig-bytes")
	pub := []byte("pub-bytes")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range map[string][]byte{
		"jetkvm_app_offline_update/jetkvm_app":        binary,
		"jetkvm_app_offline_update/jetkvm_app.sha256": []byte(hash + "  jetkvm_app\n"),
		"jetkvm_app_offline_update/jetkvm_app.sig":    sig,
		"jetkvm_app_offline_update/jetkvm_app.pub":    pub,
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
	assert.Equal(t, pub, bundle.PublicKeyData)
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

// --- VerifyOfflineBundle tests ---

// newOfflineSigningFixture generates a GPG key pair and returns a GPGVerifier
// with the correct root fingerprint and the entity for signing.
// Unlike the keyserver-based fixture, this verifier doesn't need a mock HTTP
// client — offline verification uses VerifySignatureFromFileWithKey directly.
func newOfflineSigningFixture(t *testing.T) (*GPGVerifier, *openpgp.Entity) {
	t.Helper()

	entity, err := openpgp.NewEntity("Offline Test", "", "offline@test.local", nil)
	require.NoError(t, err)

	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	v := &GPGVerifier{
		logger:    &logger,
		rootKeyFP: strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint[:])),
	}

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
func writeBundle(t *testing.T, binary, sig, pub []byte) *OfflineBundle {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jetkvm_app")
	require.NoError(t, os.WriteFile(path, binary, 0644))
	return &OfflineBundle{
		BinaryPath:    path,
		ExpectedHash:  sha256hex(binary),
		Signature:     sig,
		PublicKeyData: pub,
		Component:     "app",
	}
}

func TestVerifyOfflineBundle_ValidSignature(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("valid-app-binary-content")
	sig := signData(t, entity, binary)
	bundle := writeBundle(t, binary, sig, pub)

	result, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK, "hash should pass")
	assert.True(t, result.SignatureOK, "signature should pass")
}

func TestVerifyOfflineBundle_HashMismatch(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("real-binary")
	sig := signData(t, entity, binary)
	bundle := writeBundle(t, binary, sig, pub)

	bundle.ExpectedHash = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestVerifyOfflineBundle_InvalidSignature(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("the-real-binary")
	differentContent := []byte("tampered-binary")

	sig := signData(t, entity, differentContent)
	bundle := writeBundle(t, binary, sig, pub)

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

func TestVerifyOfflineBundle_WrongKey(t *testing.T) {
	gpgVerifier, _ := newOfflineSigningFixture(t)

	// Sign with a completely different key pair
	otherEntity, err := openpgp.NewEntity("Attacker", "", "evil@attacker.com", nil)
	require.NoError(t, err)
	otherPub := armorPublicKey(t, otherEntity)

	binary := []byte("innocent-looking-binary")
	sig := signData(t, otherEntity, binary)
	// Bundle includes the attacker's pub key, which won't match the pinned fingerprint
	bundle := writeBundle(t, binary, sig, otherPub)

	_, err = VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bundled public key rejected")
}

func TestVerifyOfflineBundle_EmptySignature(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("unsigned-binary")
	bundle := writeBundle(t, binary, nil, pub)
	bundle.Signature = nil

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is required")
}

func TestVerifyOfflineBundle_EmptyPublicKey(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)

	binary := []byte("binary-without-key")
	sig := signData(t, entity, binary)
	bundle := writeBundle(t, binary, sig, nil)
	bundle.PublicKeyData = nil

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public key is required")
}

func TestVerifyOfflineBundle_TruncatedSignature(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("binary-with-truncated-sig")
	fullSig := signData(t, entity, binary)

	truncatedSig := fullSig[:len(fullSig)/2]
	bundle := writeBundle(t, binary, truncatedSig, pub)

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPG signature verification failed")
}

func TestVerifyOfflineBundle_CorruptedBinary(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	originalBinary := []byte("original-binary-content")
	sig := signData(t, entity, originalBinary)

	bundle := writeBundle(t, originalBinary, sig, pub)
	require.NoError(t, os.WriteFile(bundle.BinaryPath, []byte("corrupted-binary"), 0644))

	_, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
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
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("end-to-end-test-binary-content-here")
	sig := signData(t, entity, binary)

	archive := buildAppArchive(t, binary, sig, pub)

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	result, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK)
	assert.True(t, result.SignatureOK)
}

// TestEndToEnd_ExtractAndVerify_TamperedBinary builds a valid archive then
// overwrites the extracted binary before verification — simulating
// file-level tampering after extraction.
func TestEndToEnd_ExtractAndVerify_TamperedBinary(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("legitimate-binary")
	sig := signData(t, entity, binary)

	archive := buildAppArchive(t, binary, sig, pub)

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(bundle.BinaryPath, []byte("tampered!"), 0644))

	_, err = VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

// TestEndToEnd_ExtractAndVerify_WrongSignature builds an archive where
// the signature was produced by a different key than the verifier expects.
func TestEndToEnd_ExtractAndVerify_WrongSignature(t *testing.T) {
	gpgVerifier, _ := newOfflineSigningFixture(t) // verifier expects key A

	attackerEntity, err := openpgp.NewEntity("Attacker", "", "evil@example.com", nil)
	require.NoError(t, err)
	attackerPub := armorPublicKey(t, attackerEntity)

	binary := []byte("innocuous-binary")
	sig := signData(t, attackerEntity, binary) // signed with key B

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(sha256hex(binary)),
		"jetkvm_app.sig":    sig,
		"jetkvm_app.pub":    attackerPub,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)

	_, err = VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bundled public key rejected")
}

// TestEndToEnd_ExtractAndVerify_HashMismatchInArchive builds an archive
// where the .sha256 file contains the wrong hash for the binary.
func TestEndToEnd_ExtractAndVerify_HashMismatchInArchive(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("real-binary-content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"jetkvm_app":        binary,
		"jetkvm_app.sha256": []byte(wrongHash),
		"jetkvm_app.sig":    sig,
		"jetkvm_app.pub":    pub,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "app", testLogger())
	require.NoError(t, err)
	assert.Equal(t, wrongHash, bundle.ExpectedHash)

	_, err = VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

// TestEndToEnd_SystemArchive verifies the full pipeline works for system
// component archives with the different expected file names.
func TestEndToEnd_SystemArchive(t *testing.T) {
	gpgVerifier, entity := newOfflineSigningFixture(t)
	pub := armorPublicKey(t, entity)

	binary := []byte("system-image-tar-content")
	hash := sha256hex(binary)
	sig := signData(t, entity, binary)

	archive := buildArchive(t, map[string][]byte{
		"update_system.tar":        binary,
		"update_system.tar.sha256": []byte(hash),
		"update_system.tar.sig":    sig,
		"update_system.tar.pub":    pub,
	})

	extractDir := t.TempDir()
	bundle, err := ExtractOfflineArchive(archive, extractDir, "system", testLogger())
	require.NoError(t, err)
	assert.Equal(t, "system", bundle.Component)

	result, err := VerifyOfflineBundle(bundle, gpgVerifier, testLogger())
	require.NoError(t, err)
	assert.True(t, result.HashOK)
	assert.True(t, result.SignatureOK)
}
