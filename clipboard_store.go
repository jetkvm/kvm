package kvm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// ClipboardStore manages clipboard files served via the main HTTPS server on port 443.
// This avoids needing a separate server on a different port.
type ClipboardStore struct {
	mu              sync.Mutex
	pending         map[string]*clipboardFile
	tempDir         string
	expiry          time.Duration
	cleanupInterval time.Duration
	stopCh          chan struct{}
	logger          *zerolog.Logger
}

type clipboardFile struct {
	path      string
	name      string
	size      int64
	createdAt time.Time
	used      bool
}

var (
	clipboardStore     *ClipboardStore
	clipboardStoreOnce sync.Once
)

// GetClipboardStore returns the global clipboard store singleton.
func GetClipboardStore() *ClipboardStore {
	clipboardStoreOnce.Do(func() {
		clipboardStore = &ClipboardStore{
			pending:         make(map[string]*clipboardFile),
			tempDir:         os.TempDir(),
			expiry:          5 * time.Minute,  // Default 5 minutes
			cleanupInterval: 60 * time.Second, // Default 60 seconds
			stopCh:          make(chan struct{}),
			logger:          logging.GetSubsystemLogger("clipboard"),
		}
		go clipboardStore.cleanupLoop()
	})
	return clipboardStore
}

// Configure updates the store's TTL and cleanup interval.
// Call this before using the store to apply config values.
func (s *ClipboardStore) Configure(ttlSec, cleanupSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ttlSec > 0 {
		s.expiry = time.Duration(ttlSec) * time.Second
		s.logger.Info().Int("ttl_sec", ttlSec).Msg("clipboard: configured file TTL")
	}
	if cleanupSec > 0 {
		s.cleanupInterval = time.Duration(cleanupSec) * time.Second
		s.logger.Info().Int("cleanup_sec", cleanupSec).Msg("clipboard: configured cleanup interval")
	}
}

// AddFile adds a file to the clipboard store and returns the download token.
func (s *ClipboardStore) AddFile(path, originalName string) (string, error) {
	// Generate secure token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	s.mu.Lock()
	s.pending[token] = &clipboardFile{
		path:      path,
		name:      originalName,
		size:      info.Size(),
		createdAt: time.Now(),
	}
	s.mu.Unlock()

	s.logger.Debug().
		Str("name", originalName).
		Int64("size", info.Size()).
		Str("token", token[:8]+"...").
		Msg("clipboard: added file for download")

	return token, nil
}

// RemoveFile removes a file from the store and deletes it from disk.
func (s *ClipboardStore) RemoveFile(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cf, ok := s.pending[token]; ok {
		if cf.path != "" {
			os.Remove(cf.path)
		}
		delete(s.pending, token)
	}
}

// CleanupAll removes all pending files.
func (s *ClipboardStore) CleanupAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for token, cf := range s.pending {
		if cf.path != "" {
			os.Remove(cf.path)
		}
		delete(s.pending, token)
		count++
	}
	return count
}

// HandleDownload is the gin handler for clipboard file downloads.
// Route: GET /c/:token
func (s *ClipboardStore) HandleDownload(c *gin.Context) {
	token := c.Param("token")
	if token == "" || len(token) < 8 {
		c.Status(http.StatusNotFound)
		return
	}

	s.mu.Lock()
	cf, ok := s.pending[token]
	if !ok {
		s.mu.Unlock()
		c.Status(http.StatusNotFound)
		return
	}

	// Check if already used (single-use)
	if cf.used {
		s.mu.Unlock()
		c.Status(http.StatusGone)
		return
	}

	// Check expiry
	if time.Since(cf.createdAt) > s.expiry {
		delete(s.pending, token)
		if cf.path != "" {
			os.Remove(cf.path)
		}
		s.mu.Unlock()
		c.Status(http.StatusGone)
		return
	}

	// Mark as used
	cf.used = true
	path := cf.path
	name := cf.name
	s.mu.Unlock()

	// Open file
	f, err := os.Open(path)
	if err != nil {
		s.logger.Warn().Err(err).Str("path", path).Msg("clipboard: failed to open file")
		c.Status(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Set headers
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(name)))
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")

	s.logger.Debug().
		Str("name", name).
		Str("remote", c.ClientIP()).
		Msg("clipboard: serving file")

	// Stream file
	if _, err := io.Copy(c.Writer, f); err != nil {
		s.logger.Debug().Err(err).Msg("clipboard: error streaming file")
	}

	// Cleanup after use (single-use mode)
	s.mu.Lock()
	delete(s.pending, token)
	s.mu.Unlock()

	if err := os.Remove(path); err != nil {
		s.logger.Warn().Err(err).Str("path", path).Msg("clipboard: failed to delete file after download")
	} else {
		s.logger.Info().Str("path", path).Str("name", name).Msg("clipboard: file served and deleted")
	}
}

func (s *ClipboardStore) cleanupLoop() {
	s.mu.Lock()
	interval := s.cleanupInterval
	s.mu.Unlock()

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-timer.C:
			s.cleanupExpired()
		}

		s.mu.Lock()
		interval = s.cleanupInterval
		s.mu.Unlock()
		timer.Reset(interval)
	}
}

func (s *ClipboardStore) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, cf := range s.pending {
		// Skip files that are currently being downloaded
		if cf.used {
			continue
		}
		if now.Sub(cf.createdAt) > s.expiry {
			if cf.path != "" {
				os.Remove(cf.path)
			}
			delete(s.pending, token)
			s.logger.Debug().
				Str("name", cf.name).
				Dur("age", now.Sub(cf.createdAt)).
				Msg("clipboard: cleaned up expired file")
		}
	}
}

// GenerateDownloadURL creates the download URL for a file token.
func GenerateDownloadURL(scheme, serverIP string, port int, token string) string {
	if port == 443 {
		return fmt.Sprintf("%s://%s/c/%s", scheme, serverIP, token)
	}
	return fmt.Sprintf("%s://%s:%d/c/%s", scheme, serverIP, port, token)
}

// GenerateDownloadCommand generates a download command for the target OS.
func GenerateDownloadCommand(targetOS, scheme, serverIP string, port int, token, fileName, customTemplate string) string {
	url := GenerateDownloadURL(scheme, serverIP, port, token)
	escapedName := escapeFileName(fileName, targetOS)

	// Use custom template if provided
	if customTemplate != "" {
		cmd := strings.ReplaceAll(customTemplate, "{url}", url)
		cmd = strings.ReplaceAll(cmd, "{filename}", escapedName)
		return cmd
	}

	// Default templates - note: for HTTPS with self-signed certs, we need -k/--insecure
	insecureFlag := ""
	if scheme == "https" {
		insecureFlag = "-k " // Skip certificate validation for self-signed
	}

	switch targetOS {
	case "windows":
		// PowerShell - works on Windows 10+
		if scheme == "https" {
			return fmt.Sprintf("[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12;iwr %s -OutFile %s -SkipCertificateCheck", url, escapedName)
		}
		return fmt.Sprintf("iwr %s -OutFile %s", url, escapedName)

	case "linux", "macos":
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)

	default:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)
	}
}

// escapeFileName escapes a filename for use in shell commands.
func escapeFileName(name, targetOS string) string {
	// Remove path
	name = filepath.Base(name)

	// For Windows PowerShell, wrap in quotes if contains spaces
	if targetOS == "windows" {
		if strings.ContainsAny(name, " '\"") {
			return "'" + strings.ReplaceAll(name, "'", "''") + "'"
		}
		return name
	}

	// For Unix shells, wrap in quotes if contains special chars
	if strings.ContainsAny(name, " '\"$`\\!") {
		return "'" + strings.ReplaceAll(name, "'", "'\\''") + "'"
	}
	return name
}
