//go:build linux

package tailscale

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPathSafe(t *testing.T) {
	basePath := "/userdata/meshvpn/tailscale"

	tests := []struct {
		name       string
		targetPath string
		wantErr    bool
	}{
		{
			name:       "valid file in base",
			targetPath: filepath.Join(basePath, "tailscale"),
			wantErr:    false,
		},
		{
			name:       "valid nested file",
			targetPath: filepath.Join(basePath, "bin", "tailscale"),
			wantErr:    false,
		},
		{
			name:       "base directory itself",
			targetPath: basePath,
			wantErr:    false,
		},
		{
			name:       "path traversal with ../",
			targetPath: filepath.Join(basePath, "..", "etc", "passwd"),
			wantErr:    true,
		},
		{
			name:       "path traversal escaping completely",
			targetPath: "/etc/passwd",
			wantErr:    true,
		},
		{
			name:       "path traversal with nested ../",
			targetPath: filepath.Join(basePath, "subdir", "..", "..", "etc", "passwd"),
			wantErr:    true,
		},
		{
			name:       "sibling directory",
			targetPath: "/userdata/meshvpn/other",
			wantErr:    true,
		},
		{
			name:       "similar prefix but different dir",
			targetPath: "/userdata/meshvpn/tailscale-evil/file",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := isPathSafe(basePath, tc.targetPath)
			if tc.wantErr {
				assert.Error(t, err, "isPathSafe should reject path: %s", tc.targetPath)
			} else {
				assert.NoError(t, err, "isPathSafe should allow path: %s", tc.targetPath)
			}
		})
	}
}
