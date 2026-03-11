package ota

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGPGVerifier() *GPGVerifier {
	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	mockClient := func() HttpClient {
		return &failingHTTPClient{callCount: &atomic.Int32{}}
	}
	return NewGPGVerifier(&logger, mockClient)
}

// generateTestArmoredKey creates a valid armored PGP public key for testing.
func generateTestArmoredKey(t *testing.T) []byte {
	t.Helper()
	entity, err := openpgp.NewEntity("Test User", "test", "test@example.com", nil)
	require.NoError(t, err, "failed to generate test PGP entity")

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err, "failed to create armor encoder")
	require.NoError(t, entity.Serialize(w), "failed to serialize public key")
	require.NoError(t, w.Close(), "failed to close armor writer")

	return buf.Bytes()
}

func extractFingerprintFromArmoredKey(t *testing.T, armoredKey []byte) string {
	t.Helper()

	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armoredKey))
	require.NoError(t, err, "failed to parse armored key")
	require.NotEmpty(t, keyring, "parsed keyring should not be empty")
	require.NotNil(t, keyring[0].PrimaryKey, "primary key should be present")

	return strings.ToUpper(hex.EncodeToString(keyring[0].PrimaryKey.Fingerprint[:]))
}

// keyServingHTTPClient is a mock HTTP client that serves an armored key and counts requests.
type keyServingHTTPClient struct {
	key       []byte
	callCount *atomic.Int32
}

func (c *keyServingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.callCount.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(c.key)),
	}, nil
}

// failingHTTPClient always returns an error.
type failingHTTPClient struct {
	callCount *atomic.Int32
}

func (c *failingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.callCount.Add(1)
	return nil, fmt.Errorf("connection refused")
}

// statusCodeHTTPClient returns a configurable status code per call index.
type statusCodeHTTPClient struct {
	key         []byte
	statusCodes []int // status code for each sequential call
	callCount   *atomic.Int32
}

func (c *statusCodeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	idx := int(c.callCount.Add(1)) - 1
	status := http.StatusInternalServerError
	if idx < len(c.statusCodes) {
		status = c.statusCodes[idx]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(c.key)),
	}, nil
}

// newGPGVerifierWithMock creates a GPGVerifier with the given mock HTTP client factory.
// It also overrides the keyservers list to use a single test URL pattern.
func newGPGVerifierWithMock(t *testing.T, clientFactory func() HttpClient) *GPGVerifier {
	t.Helper()
	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
	v := NewGPGVerifier(&logger, clientFactory)

	// Override keyservers to a single test pattern so we control all requests
	t.Cleanup(func() {
		keyservers = []string{
			"https://keys.openpgp.org/vks/v1/by-fingerprint/%s",
			"https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x%s",
		}
	})
	keyservers = []string{"https://test-keyserver.example.com/keys/%s"}

	return v
}

func TestFetchPublicKey_CachesKey(t *testing.T) {
	armoredKey := generateTestArmoredKey(t)
	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: armoredKey, callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	ctx := context.Background()

	// First call should fetch from the "keyserver"
	key1, err := v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.NotNil(t, key1)
	assert.Equal(t, int32(1), callCount.Load(), "first call should hit the keyserver")

	// Second call should use the cache
	key2, err := v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.NotNil(t, key2)
	assert.Equal(t, int32(1), callCount.Load(), "second call should use cache, not hit keyserver")

	// Both calls should return the same key
	assert.Equal(t, key1, key2, "cached key should match original")
}

func TestFetchPublicKey_CacheExpiry(t *testing.T) {
	armoredKey := generateTestArmoredKey(t)
	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: armoredKey, callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	ctx := context.Background()

	// First call - populates cache
	_, err := v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// Simulate cache expiry by backdating the cache time
	v.mu.Lock()
	v.cachedKeyTime = time.Now().Add(-25 * time.Hour) // past the 24h TTL
	v.mu.Unlock()

	// Next call should re-fetch since cache is expired
	_, err = v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "should re-fetch after cache expiry")
}

func TestClearCache(t *testing.T) {
	armoredKey := generateTestArmoredKey(t)
	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: armoredKey, callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	ctx := context.Background()

	// Populate cache
	_, err := v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// Verify cache is populated
	v.mu.RLock()
	assert.NotNil(t, v.cachedKey, "cache should be populated")
	assert.NotNil(t, v.keyring, "keyring should be populated")
	v.mu.RUnlock()

	// Clear cache
	v.ClearCache()

	// Verify cache is cleared
	v.mu.RLock()
	assert.Nil(t, v.cachedKey, "cache should be cleared")
	assert.Nil(t, v.keyring, "keyring should be cleared")
	assert.True(t, v.cachedKeyTime.IsZero(), "cache time should be zero")
	v.mu.RUnlock()

	// Next fetch should hit the keyserver again
	_, err = v.FetchPublicKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "should re-fetch after ClearCache")
}

func TestUpdateMemoryCache_StoresKeyAndKeyring(t *testing.T) {
	armoredKey := generateTestArmoredKey(t)
	fp := extractFingerprintFromArmoredKey(t, armoredKey)

	v := newTestGPGVerifier()
	v.rootKeyFP = fp

	// Pre-validate a keyring and cache it
	keyring, err := v.parseAndValidateKeyring(armoredKey)
	require.NoError(t, err)

	v.updateMemoryCache(armoredKey, keyring)

	// Verify it was cached
	v.mu.RLock()
	assert.NotNil(t, v.cachedKey, "key should be cached")
	assert.NotNil(t, v.keyring, "keyring should be cached")
	assert.False(t, v.cachedKeyTime.IsZero(), "cache time should be set")
	v.mu.RUnlock()
}

func TestFetchPublicKey_KeyserverFallback(t *testing.T) {
	armoredKey := generateTestArmoredKey(t)
	callCount := &atomic.Int32{}
	mock := &statusCodeHTTPClient{
		key:         armoredKey,
		statusCodes: []int{http.StatusInternalServerError, http.StatusOK},
		callCount:   callCount,
	}

	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)
	v := NewGPGVerifier(&logger, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, armoredKey)

	// Override keyservers to have two entries so the fallback is exercised
	origKeyservers := keyservers
	t.Cleanup(func() { keyservers = origKeyservers })
	keyservers = []string{
		"https://bad-keyserver.example.com/keys/%s",
		"https://good-keyserver.example.com/keys/%s",
	}

	ctx := context.Background()
	key, err := v.FetchPublicKey(ctx)
	require.NoError(t, err, "should succeed via fallback keyserver")
	assert.NotNil(t, key)
	assert.Equal(t, int32(2), callCount.Load(), "should have tried both keyservers")
}

func TestFetchPublicKey_AllKeyserversFail(t *testing.T) {
	callCount := &atomic.Int32{}
	mock := &failingHTTPClient{callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })

	ctx := context.Background()
	key, err := v.FetchPublicKey(ctx)
	assert.Error(t, err, "should fail when all keyservers fail")
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "all keyservers failed")
	assert.Equal(t, int32(1), callCount.Load(), "should have tried the one test keyserver")
}

func TestFetchPublicKey_CachedKeyIsValid(t *testing.T) {
	// Generate a test key pair (keep the entity so we can sign with the private key)
	entity, err := openpgp.NewEntity("Test", "", "test@example.com", nil)
	require.NoError(t, err)

	// Armor the public key (this is what the mock "keyserver" serves)
	var pubBuf bytes.Buffer
	w, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())

	// Set up verifier with mock serving this key
	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: pubBuf.Bytes(), callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, pubBuf.Bytes())

	// Fetch key (populates cache + keyring)
	ctx := context.Background()
	_, err = v.FetchPublicKey(ctx)
	require.NoError(t, err)

	// Sign some data with the private key
	testData := []byte("hello world")
	var sigBuf bytes.Buffer
	err = openpgp.DetachSign(&sigBuf, entity, bytes.NewReader(testData), nil)
	require.NoError(t, err)

	// Verify the signature using the cached keyring —
	// this proves the cache holds a valid, usable key
	err = v.VerifySignature(ctx, sigBuf.Bytes(), testData)
	assert.NoError(t, err, "cached keyring should verify a valid signature")

	// Verify it was served from cache (no extra HTTP call)
	assert.Equal(t, int32(1), callCount.Load(), "VerifySignature should use cached key")
}

func TestFetchPublicKey_RejectsFingerprintMismatch(t *testing.T) {
	expectedKey := generateTestArmoredKey(t)
	servedKey := generateTestArmoredKey(t)

	callCount := &atomic.Int32{}
	mock := &keyServingHTTPClient{key: servedKey, callCount: callCount}
	v := newGPGVerifierWithMock(t, func() HttpClient { return mock })
	v.rootKeyFP = extractFingerprintFromArmoredKey(t, expectedKey)

	ctx := context.Background()
	key, err := v.FetchPublicKey(ctx)
	require.Error(t, err, "fetch should fail when fetched key fingerprint doesn't match pinned fingerprint")
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "does not match expected fingerprint")
	assert.Equal(t, int32(1), callCount.Load(), "should have tried keyserver once")
}
