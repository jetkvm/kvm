package tailscale

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		new      string
		expected bool
	}{
		// Basic version comparisons
		{
			name:     "basic upgrade",
			current:  "1.88.1",
			new:      "1.92.3",
			expected: true,
		},
		{
			name:     "basic downgrade",
			current:  "1.92.3",
			new:      "1.88.1",
			expected: false,
		},
		{
			name:     "same version",
			current:  "1.92.3",
			new:      "1.92.3",
			expected: false,
		},

		// Critical case: comparing 1.9 vs 1.10 (string comparison would fail)
		{
			name:     "1.9 to 1.10 upgrade",
			current:  "1.9.0",
			new:      "1.10.0",
			expected: true,
		},
		{
			name:     "1.10 to 1.9 downgrade",
			current:  "1.10.0",
			new:      "1.9.0",
			expected: false,
		},

		// With v prefix
		{
			name:     "v prefix upgrade",
			current:  "v1.88.1",
			new:      "v1.92.3",
			expected: true,
		},
		{
			name:     "v prefix downgrade",
			current:  "v1.92.3",
			new:      "v1.88.1",
			expected: false,
		},
		{
			name:     "mixed v prefix",
			current:  "1.88.1",
			new:      "v1.92.3",
			expected: true,
		},

		// Different version lengths
		{
			name:     "shorter to longer version",
			current:  "1.92",
			new:      "1.92.3",
			expected: true,
		},
		{
			name:     "longer to shorter version",
			current:  "1.92.3",
			new:      "1.92",
			expected: false,
		},

		// Pre-release suffixes (should compare base versions only)
		{
			name:     "stable to beta of newer",
			current:  "1.92.3",
			new:      "1.92.4-beta",
			expected: true,
		},
		{
			name:     "beta to same stable",
			current:  "1.92.3-beta",
			new:      "1.92.3",
			expected: false,
		},
		{
			name:     "beta to newer stable",
			current:  "1.92.3-beta",
			new:      "1.92.4",
			expected: true,
		},
		{
			name:     "beta of newer to older stable",
			current:  "1.92.4-beta",
			new:      "1.92.3",
			expected: false,
		},

		// Build metadata
		{
			name:     "with build metadata",
			current:  "1.92.3",
			new:      "1.92.4+build123",
			expected: true,
		},
		{
			name:     "same version different builds",
			current:  "1.92.3+build1",
			new:      "1.92.3+build2",
			expected: false,
		},

		// Edge cases
		{
			name:     "major version upgrade",
			current:  "1.92.3",
			new:      "2.0.0",
			expected: true,
		},
		{
			name:     "single digit versions",
			current:  "1",
			new:      "2",
			expected: true,
		},

		// Invalid versions (should return false)
		{
			name:     "invalid current version",
			current:  "invalid",
			new:      "1.92.3",
			expected: false,
		},
		{
			name:     "invalid new version",
			current:  "1.92.3",
			new:      "invalid",
			expected: false,
		},
		{
			name:     "both invalid",
			current:  "invalid",
			new:      "also-invalid",
			expected: false,
		},
		{
			name:     "empty versions",
			current:  "",
			new:      "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsNewerVersion(tc.current, tc.new)
			assert.Equal(t, tc.expected, result,
				"IsNewerVersion(%q, %q) should be %v", tc.current, tc.new, tc.expected)
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int
		ok       bool
	}{
		{
			name:     "simple version",
			input:    "1.92.3",
			expected: []int{1, 92, 3},
			ok:       true,
		},
		{
			name:     "with v prefix",
			input:    "v1.92.3",
			expected: []int{1, 92, 3},
			ok:       true,
		},
		{
			name:     "with pre-release suffix",
			input:    "1.92.3-beta",
			expected: []int{1, 92, 3},
			ok:       true,
		},
		{
			name:     "with build metadata",
			input:    "1.92.3+build123",
			expected: []int{1, 92, 3},
			ok:       true,
		},
		{
			name:     "two part version",
			input:    "1.92",
			expected: []int{1, 92},
			ok:       true,
		},
		{
			name:     "single part version",
			input:    "1",
			expected: []int{1},
			ok:       true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			ok:       false,
		},
		{
			name:     "non-numeric part",
			input:    "1.x.3",
			expected: nil,
			ok:       false,
		},
		{
			name:     "only prefix",
			input:    "v",
			expected: nil,
			ok:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := parseVersion(tc.input)
			assert.Equal(t, tc.ok, ok, "parseVersion(%q) ok should be %v", tc.input, tc.ok)
			if tc.ok {
				assert.Equal(t, tc.expected, result,
					"parseVersion(%q) should be %v", tc.input, tc.expected)
			}
		})
	}
}
