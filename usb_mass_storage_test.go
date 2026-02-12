package kvm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{"Simple filename", "image.iso", "image.iso", false},
		{"Filename with spaces", "my image.iso", "my image.iso", false},
		{"Path traversal", "../image.iso", "", true},
		{"Absolute path", "/etc/passwd", "", true},
		{"Current directory", ".", "", true},
		{"Parent directory", "..", "", true},
		{"Root directory", "/", "", true},
		{"Nested path", "folder/image.iso", "image.iso", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeFilename(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeFilename() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("sanitizeFilename() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRpcDownloadFromUrl(t *testing.T) {
	// Create temp directory for images
	tmpDir := t.TempDir()
	originalImagesFolder := imagesFolder
	imagesFolder = tmpDir
	defer func() { imagesFolder = originalImagesFolder }()

	// Start test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.Write([]byte("test content"))
	}))
	defer ts.Close()

	// Test download
	filename := "test.iso"
	err := rpcDownloadFromUrl(ts.URL, filename)
	if err != nil {
		t.Fatalf("rpcDownloadFromUrl() error = %v", err)
	}

	// Wait for download to complete (since it's async)
	// In a real test we might need a better way to wait, but for now we can poll the state
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for download")
		case <-ticker.C:
			state, _ := rpcGetDownloadState()
			if state.Error != "" {
				t.Fatalf("download failed with error: %s", state.Error)
			}
			if !state.Downloading && state.DoneBytes == 12 {
				goto Done
			}
		}
	}
Done:

	// Verify file content
	content, err := os.ReadFile(filepath.Join(tmpDir, filename))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("file content = %q, want %q", string(content), "test content")
	}
}
