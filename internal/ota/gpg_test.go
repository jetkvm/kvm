package ota

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func newTestGPGVerifier() *GPGVerifier {
	logger := zerolog.New(os.Stdout).Level(zerolog.WarnLevel)
	return NewGPGVerifier(&logger, nil)
}

func TestIsSignatureRequired_Upgrade(t *testing.T) {
	v := newTestGPGVerifier()

	// Major version upgrade
	assert.True(t, v.IsSignatureRequired("1.0.0", "2.0.0"), "major upgrade should require signature")

	// Minor version upgrade
	assert.True(t, v.IsSignatureRequired("1.0.0", "1.1.0"), "minor upgrade should require signature")

	// Patch version upgrade
	assert.True(t, v.IsSignatureRequired("1.0.0", "1.0.1"), "patch upgrade should require signature")
}

func TestIsSignatureRequired_Downgrade(t *testing.T) {
	v := newTestGPGVerifier()

	// Major version downgrade
	assert.False(t, v.IsSignatureRequired("2.0.0", "1.0.0"), "major downgrade should NOT require signature")

	// Minor version downgrade
	assert.False(t, v.IsSignatureRequired("1.1.0", "1.0.0"), "minor downgrade should NOT require signature")

	// Patch version downgrade
	assert.False(t, v.IsSignatureRequired("1.0.1", "1.0.0"), "patch downgrade should NOT require signature")
}

func TestIsSignatureRequired_SameVersion(t *testing.T) {
	v := newTestGPGVerifier()

	assert.False(t, v.IsSignatureRequired("1.0.0", "1.0.0"), "same version should NOT require signature")
	assert.False(t, v.IsSignatureRequired("2.5.3", "2.5.3"), "same version should NOT require signature")
}

func TestIsSignatureRequired_InvalidLocalVersion(t *testing.T) {
	v := newTestGPGVerifier()

	// Invalid local version should fail-safe to requiring signature
	assert.True(t, v.IsSignatureRequired("invalid", "1.0.0"), "invalid local version should require signature (fail-safe)")
	assert.True(t, v.IsSignatureRequired("", "1.0.0"), "empty local version should require signature (fail-safe)")
	assert.True(t, v.IsSignatureRequired("not-semver", "2.0.0"), "non-semver local version should require signature (fail-safe)")
}

func TestIsSignatureRequired_InvalidTargetVersion(t *testing.T) {
	v := newTestGPGVerifier()

	// Invalid target version should fail-safe to requiring signature
	assert.True(t, v.IsSignatureRequired("1.0.0", "invalid"), "invalid target version should require signature (fail-safe)")
	assert.True(t, v.IsSignatureRequired("1.0.0", ""), "empty target version should require signature (fail-safe)")
	assert.True(t, v.IsSignatureRequired("2.0.0", "not-semver"), "non-semver target version should require signature (fail-safe)")
}

func TestIsSignatureRequired_PreReleaseVersions(t *testing.T) {
	v := newTestGPGVerifier()

	// Upgrade to pre-release should require signature
	assert.True(t, v.IsSignatureRequired("1.0.0", "1.0.1-dev.1"), "upgrade to pre-release should require signature")
	assert.True(t, v.IsSignatureRequired("1.0.0", "1.1.0-alpha"), "upgrade to alpha should require signature")
	assert.True(t, v.IsSignatureRequired("1.0.0", "2.0.0-beta.1"), "upgrade to beta should require signature")

	// Pre-release to stable upgrade should require signature
	assert.True(t, v.IsSignatureRequired("1.0.0-dev.1", "1.0.0"), "pre-release to stable should require signature")

	// Pre-release to newer pre-release should require signature
	assert.True(t, v.IsSignatureRequired("1.0.0-dev.1", "1.0.0-dev.2"), "older pre-release to newer pre-release should require signature")

	// Downgrade between pre-releases should NOT require signature
	assert.False(t, v.IsSignatureRequired("1.0.0-dev.2", "1.0.0-dev.1"), "newer pre-release to older pre-release should NOT require signature")
}
