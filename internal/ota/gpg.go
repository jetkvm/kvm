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
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/rs/zerolog"
)

// rootKeyFingerprint is the GPG fingerprint of the JetKVM release root key.
// This key is used to verify signatures on OTA updates.
const rootKeyFingerprint = "AF5A36A993D828FEFE7C18C2D1B9856C26A79E95"

// keyservers is the ordered list of keyservers to try when fetching public keys.
// We try each in order and return on first success.
var keyservers = []string{
	"https://keys.openpgp.org/vks/v1/by-fingerprint/%s",
	"https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x%s",
	// "https://pgp.mit.edu/pks/lookup?op=get&search=0x%s",
}

const (
	// keyCacheTTL is how long to cache the public key before refreshing
	keyCacheTTL = 24 * time.Hour
	// keyFetchTimeout is the timeout for fetching a key from a single keyserver
	keyFetchTimeout = 30 * time.Second
)

// GPGVerifier handles GPG signature verification for OTA updates
type GPGVerifier struct {
	mu            sync.RWMutex
	cachedKey     []byte
	cachedKeyTime time.Time
	keyring       openpgp.EntityList
	logger        *zerolog.Logger
	httpClient    func() HttpClient
	rootKeyFP     string
}

// NewGPGVerifier creates a new GPG verifier instance
func NewGPGVerifier(logger *zerolog.Logger, httpClient func() HttpClient) *GPGVerifier {
	return &GPGVerifier{
		logger:     logger,
		httpClient: httpClient,
		rootKeyFP:  rootKeyFingerprint,
	}
}

// GetRootKeyFingerprint returns the configured root key fingerprint
func (g *GPGVerifier) GetRootKeyFingerprint() string {
	return g.rootKeyFP
}

// FetchPublicKey fetches the public key from keyservers with fallback support.
// It tries each keyserver in order and returns on first success.
// The key is cached for 24 hours.
func (g *GPGVerifier) FetchPublicKey(ctx context.Context) ([]byte, error) {
	if g.rootKeyFP == "" {
		return nil, fmt.Errorf("root key fingerprint not configured")
	}

	// Check memory cache first
	g.mu.RLock()
	if g.cachedKey != nil && time.Since(g.cachedKeyTime) < keyCacheTTL {
		key := make([]byte, len(g.cachedKey))
		copy(key, g.cachedKey)
		g.mu.RUnlock()
		g.logger.Debug().Msg("using cached public key from memory")
		return key, nil
	}
	g.mu.RUnlock()

	// Fetch from keyservers (returns already-validated keyring)
	key, keyring, err := g.fetchFromKeyservers(ctx, g.rootKeyFP)
	if err != nil {
		return nil, err
	}

	// Cache the key and pre-validated keyring
	g.updateMemoryCache(key, keyring)

	return key, nil
}

// fetchFromKeyservers tries each keyserver in order and returns on first success.
// Returns the raw key bytes and the already-validated keyring.
func (g *GPGVerifier) fetchFromKeyservers(ctx context.Context, fingerprint string) ([]byte, openpgp.EntityList, error) {
	var errors []error

	for _, serverTemplate := range keyservers {
		// Check if context was cancelled before trying next server
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("key fetch cancelled: %w", err)
		}

		url := fmt.Sprintf(serverTemplate, fingerprint)
		g.logger.Debug().Str("url", url).Msg("trying keyserver")

		key, keyring, err := g.fetchFromSingleKeyserver(ctx, url)
		if err != nil {
			g.logger.Debug().Err(err).Str("url", url).Msg("keyserver failed")
			errors = append(errors, fmt.Errorf("%s: %w", url, err))
			continue
		}

		g.logger.Info().Str("url", url).Msg("successfully fetched public key")
		return key, keyring, nil
	}

	// All keyservers failed - check if it was due to cancellation
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("key fetch cancelled after trying all servers: %w", err)
	}

	return nil, nil, fmt.Errorf("all keyservers failed: %v", errors)
}

// fetchFromSingleKeyserver fetches the public key from a single keyserver
func (g *GPGVerifier) fetchFromSingleKeyserver(ctx context.Context, url string) ([]byte, openpgp.EntityList, error) {
	ctx, cancel := context.WithTimeout(ctx, keyFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating request: %w", err)
	}

	client := g.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("keyserver returned status %d", resp.StatusCode)
	}

	key, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading response: %w", err)
	}

	// Validate format and verify the fetched key matches our pinned fingerprint.
	keyring, err := g.parseAndValidateKeyring(key)
	if err != nil {
		return nil, nil, err
	}

	return key, keyring, nil
}

// updateMemoryCache updates the in-memory key cache with a pre-validated keyring.
func (g *GPGVerifier) updateMemoryCache(key []byte, keyring openpgp.EntityList) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.cachedKey = make([]byte, len(key))
	copy(g.cachedKey, key)
	g.cachedKeyTime = time.Now()
	g.keyring = keyring
}

func normalizeFingerprint(fp string) string {
	fp = strings.ToUpper(strings.TrimSpace(fp))
	fp = strings.TrimPrefix(fp, "0X")
	fp = strings.ReplaceAll(fp, " ", "")
	return fp
}

func (g *GPGVerifier) parseAndValidateKeyring(key []byte) (openpgp.EntityList, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil {
		return nil, fmt.Errorf("invalid OpenPGP key: %w", err)
	}

	if len(keyring) == 0 {
		return nil, fmt.Errorf("invalid OpenPGP key: keyring is empty")
	}

	expected := normalizeFingerprint(g.rootKeyFP)
	for _, entity := range keyring {
		if entity == nil || entity.PrimaryKey == nil {
			continue
		}

		fp := normalizeFingerprint(hex.EncodeToString(entity.PrimaryKey.Fingerprint[:]))
		if fp == expected {
			return keyring, nil
		}
	}

	return nil, fmt.Errorf("fetched key fingerprint does not match expected fingerprint %s", expected)
}

// getKeyring ensures the public key is fetched and returns the keyring.
func (g *GPGVerifier) getKeyring(ctx context.Context) (openpgp.EntityList, error) {
	if _, err := g.FetchPublicKey(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch public key: %w", err)
	}

	g.mu.RLock()
	keyring := g.keyring
	g.mu.RUnlock()

	if keyring == nil {
		return nil, fmt.Errorf("keyring not initialized")
	}
	return keyring, nil
}

// VerifySignature verifies a detached GPG signature against the provided data.
// The signature should be in binary format (not armored).
func (g *GPGVerifier) VerifySignature(ctx context.Context, signature, data []byte) error {
	keyring, err := g.getKeyring(ctx)
	if err != nil {
		return err
	}

	if _, err := openpgp.CheckDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(signature), nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	g.logger.Info().Msg("signature verification successful")
	return nil
}

// VerifySignatureFromFile verifies a detached GPG signature against a file.
// This is more memory-efficient for large files.
func (g *GPGVerifier) VerifySignatureFromFile(ctx context.Context, signature []byte, filePath string) error {
	keyring, err := g.getKeyring(ctx)
	if err != nil {
		return err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for verification: %w", err)
	}
	defer file.Close()

	if _, err := openpgp.CheckDetachedSignature(keyring, file, bytes.NewReader(signature), nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	g.logger.Info().Str("file", filePath).Msg("signature verification successful")
	return nil
}

// ClearCache clears the cached public key (useful for testing)
func (g *GPGVerifier) ClearCache() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.cachedKey = nil
	g.cachedKeyTime = time.Time{}
	g.keyring = nil
}
