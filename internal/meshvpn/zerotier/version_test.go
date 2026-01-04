package zerotier

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected bool
	}{
		// Basic version comparisons
		{
			name:     "basic upgrade",
			current:  "1.14.0",
			latest:   "1.16.0",
			expected: true,
		},
		{
			name:     "basic downgrade",
			current:  "1.16.0",
			latest:   "1.14.0",
			expected: false,
		},
		{
			name:     "same version",
			current:  "1.14.0",
			latest:   "1.14.0",
			expected: false,
		},

		// Patch version comparisons
		{
			name:     "patch upgrade",
			current:  "1.14.0",
			latest:   "1.14.1",
			expected: true,
		},
		{
			name:     "patch downgrade",
			current:  "1.14.1",
			latest:   "1.14.0",
			expected: false,
		},

		// Major version comparisons
		{
			name:     "major upgrade",
			current:  "1.14.0",
			latest:   "2.0.0",
			expected: true,
		},
		{
			name:     "major downgrade",
			current:  "2.0.0",
			latest:   "1.14.0",
			expected: false,
		},

		// Critical case: comparing x.9 vs x.10
		{
			name:     "1.9 to 1.10 upgrade",
			current:  "1.9.0",
			latest:   "1.10.0",
			expected: true,
		},
		{
			name:     "1.10 to 1.9 downgrade",
			current:  "1.10.0",
			latest:   "1.9.0",
			expected: false,
		},

		// Real ZeroTier versions
		{
			name:     "real ZeroTier upgrade",
			current:  "1.14.0",
			latest:   "1.14.2",
			expected: true,
		},

		// Invalid versions (should return false)
		{
			name:     "invalid current version",
			current:  "invalid",
			latest:   "1.14.0",
			expected: false,
		},
		{
			name:     "invalid latest version",
			current:  "1.14.0",
			latest:   "invalid",
			expected: false,
		},
		{
			name:     "both invalid",
			current:  "invalid",
			latest:   "also-invalid",
			expected: false,
		},
		{
			name:     "empty versions",
			current:  "",
			latest:   "",
			expected: false,
		},
		{
			name:     "two part version (invalid for ZeroTier)",
			current:  "1.14",
			latest:   "1.15.0",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsNewerVersion(tc.current, tc.latest)
			assert.Equal(t, tc.expected, result,
				"IsNewerVersion(%q, %q) should be %v", tc.current, tc.latest, tc.expected)
		})
	}
}
