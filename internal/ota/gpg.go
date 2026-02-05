package ota

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
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
}

// NewGPGVerifier creates a new GPG verifier instance
func NewGPGVerifier(logger *zerolog.Logger, httpClient func() HttpClient) *GPGVerifier {
	return &GPGVerifier{
		logger:     logger,
		httpClient: httpClient,
	}
}

// GetRootKeyFingerprint returns the configured root key fingerprint
func (g *GPGVerifier) GetRootKeyFingerprint() string {
	return rootKeyFingerprint
}

// IsSignatureRequired returns true if the target version is greater than the local version.
// This enforces signatures for upgrades, while allowing unsigned downgrades (this is always very intentional by the user).
func (g *GPGVerifier) IsSignatureRequired(localVersion string, targetVersion string) bool {
	local, err := semver.NewVersion(localVersion)
	if err != nil {
		g.logger.Warn().
			Err(err).
			Str("localVersion", localVersion).
			Msg("failed to parse local version, requiring signature")
		return true
	}

	target, err := semver.NewVersion(targetVersion)
	if err != nil {
		g.logger.Warn().
			Err(err).
			Str("targetVersion", targetVersion).
			Msg("failed to parse target version, requiring signature")
		return true
	}

	required := target.GreaterThan(local)
	g.logger.Debug().
		Str("localVersion", localVersion).
		Str("targetVersion", targetVersion).
		Bool("signatureRequired", required).
		Msg("checked if signature is required")

	return required
}

// FetchPublicKey fetches the public key from keyservers with fallback support.
// It tries each keyserver in order and returns on first success.
// The key is cached for 24 hours.
func (g *GPGVerifier) FetchPublicKey(ctx context.Context) ([]byte, error) {
	if rootKeyFingerprint == "" {
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

	// Fetch from keyservers
	key, err := g.fetchFromKeyservers(ctx, rootKeyFingerprint)
	if err != nil {
		return nil, err
	}

	// Cache the key
	g.updateMemoryCache(key)

	return key, nil
}

// fetchFromKeyservers tries each keyserver in order and returns on first success
func (g *GPGVerifier) fetchFromKeyservers(ctx context.Context, fingerprint string) ([]byte, error) {
	var errors []error

	for _, serverTemplate := range keyservers {
		// Check if context was cancelled before trying next server
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("key fetch cancelled: %w", err)
		}

		url := fmt.Sprintf(serverTemplate, fingerprint)
		g.logger.Debug().Str("url", url).Msg("trying keyserver")

		key, err := g.fetchFromSingleKeyserver(ctx, url)
		if err != nil {
			g.logger.Debug().Err(err).Str("url", url).Msg("keyserver failed")
			errors = append(errors, fmt.Errorf("%s: %w", url, err))
			continue
		}

		g.logger.Info().Str("url", url).Msg("successfully fetched public key")
		return key, nil
	}

	// All keyservers failed - check if it was due to cancellation
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("key fetch cancelled after trying all servers: %w", err)
	}

	return nil, fmt.Errorf("all keyservers failed: %v", errors)
}

// fetchFromSingleKeyserver fetches the public key from a single keyserver
func (g *GPGVerifier) fetchFromSingleKeyserver(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, keyFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	client := g.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keyserver returned status %d", resp.StatusCode)
	}

	key, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	// Validate that this is a valid OpenPGP key
	_, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil {
		return nil, fmt.Errorf("invalid OpenPGP key: %w", err)
	}

	return key, nil
}

// updateMemoryCache updates the in-memory key cache
func (g *GPGVerifier) updateMemoryCache(key []byte) {
	// Parse the keyring first to validate before caching
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil {
		g.logger.Warn().Err(err).Msg("failed to parse keyring, not caching")
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.cachedKey = make([]byte, len(key))
	copy(g.cachedKey, key)
	g.cachedKeyTime = time.Now()
	g.keyring = keyring
}

// VerifySignature verifies a detached GPG signature against the provided data.
// The signature should be in binary format (not armored).
func (g *GPGVerifier) VerifySignature(ctx context.Context, signature, data []byte) error {
	// Ensure we have the public key
	_, err := g.FetchPublicKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch public key: %w", err)
	}

	g.mu.RLock()
	keyring := g.keyring
	g.mu.RUnlock()

	if keyring == nil {
		return fmt.Errorf("keyring not initialized")
	}

	// Verify the signature
	_, err = openpgp.CheckDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(signature), nil)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	g.logger.Info().Msg("signature verification successful")
	return nil
}

// VerifySignatureFromFile verifies a detached GPG signature against a file.
// This is more memory-efficient for large files.
func (g *GPGVerifier) VerifySignatureFromFile(ctx context.Context, signature []byte, filePath string) error {
	// Ensure we have the public key
	_, err := g.FetchPublicKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch public key: %w", err)
	}

	g.mu.RLock()
	keyring := g.keyring
	g.mu.RUnlock()

	if keyring == nil {
		return fmt.Errorf("keyring not initialized")
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for verification: %w", err)
	}
	defer file.Close()

	// Verify the signature
	_, err = openpgp.CheckDetachedSignature(keyring, file, bytes.NewReader(signature), nil)
	if err != nil {
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
