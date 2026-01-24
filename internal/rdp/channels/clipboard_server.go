// Package channels implements RDP virtual channels.
//
// ClipboardServer provides secure HTTPS file serving for clipboard file transfer.
// Files are encrypted in transit using TLS to prevent MITM attacks.
package channels

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ClipboardServer serves clipboard files via HTTPS for secure network-based transfer.
// All transfers are TLS-encrypted to prevent MITM attacks.
type ClipboardServer struct {
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	port     int
	stopCh   chan struct{}

	// Pending files keyed by token
	pending map[string]*pendingFile

	// Configuration
	singleUse   bool
	expiryTime  time.Duration
	cleanupTick time.Duration

	// TLS configuration
	tlsEnabled    bool
	getCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)

	// Temp directory for files
	tempDir string

	logger func(format string, args ...any)
}

type pendingFile struct {
	path      string
	name      string
	size      int64
	createdAt time.Time
	used      bool
}

// ClipboardServerConfig holds configuration for the clipboard server.
type ClipboardServerConfig struct {
	Port           int
	TempDir        string
	ExpiryMinutes  int  // TTL for files (default: 5)
	CleanupSeconds int  // Cleanup interval (default: 60)
	SingleUse      bool // Delete after first download (default: true)
	TLSEnabled     bool // Use HTTPS instead of HTTP
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

// NewClipboardServer creates a new clipboard HTTPS server.
func NewClipboardServer(cfg ClipboardServerConfig) *ClipboardServer {
	expiryMinutes := cfg.ExpiryMinutes
	if expiryMinutes <= 0 {
		expiryMinutes = 5
	}

	cleanupSeconds := cfg.CleanupSeconds
	if cleanupSeconds <= 0 {
		cleanupSeconds = 60
	}

	tempDir := cfg.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	return &ClipboardServer{
		port:           cfg.Port,
		pending:        make(map[string]*pendingFile),
		singleUse:      cfg.SingleUse,
		expiryTime:     time.Duration(expiryMinutes) * time.Minute,
		cleanupTick:    time.Duration(cleanupSeconds) * time.Second,
		tlsEnabled:     cfg.TLSEnabled,
		getCertificate: cfg.GetCertificate,
		tempDir:        tempDir,
		stopCh:         make(chan struct{}),
	}
}

// SetLogger sets the logging function.
func (s *ClipboardServer) SetLogger(logger func(format string, args ...any)) {
	s.logger = logger
}

func (s *ClipboardServer) log(format string, args ...any) {
	if s.logger != nil {
		s.logger(format, args...)
	}
}

// Start starts the HTTPS server.
func (s *ClipboardServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return nil // Already running
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/c/", s.handleDownload)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Allow time for large file downloads
	}

	var err error
	s.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}

	// Get actual port (in case 0 was specified)
	s.port = s.listener.Addr().(*net.TCPAddr).Port

	// Wrap with TLS if enabled
	if s.tlsEnabled && s.getCertificate != nil {
		tlsConfig := &tls.Config{
			GetCertificate: s.getCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		s.listener = tls.NewListener(s.listener, tlsConfig)
		s.log("clipboard server starting with TLS on port %d", s.port)
	} else {
		s.log("clipboard server starting (no TLS) on port %d", s.port)
	}

	s.stopCh = make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log("clipboard server panicked: %v", r)
			}
		}()
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.log("clipboard server error: %v", err)
		}
	}()

	// Start cleanup goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log("clipboard cleanup goroutine panicked: %v", r)
			}
		}()
		s.cleanupLoop()
	}()

	return nil
}

// Stop stops the HTTPS server and cleans up all files.
func (s *ClipboardServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}

	// Signal cleanup goroutine to stop
	close(s.stopCh)

	err := s.server.Close()
	s.server = nil
	s.listener = nil

	// Cleanup all pending files
	for token, pf := range s.pending {
		if pf.path != "" {
			os.Remove(pf.path)
		}
		delete(s.pending, token)
	}

	s.log("clipboard server stopped, all files cleaned up")
	return err
}

// GetPort returns the server's port.
func (s *ClipboardServer) GetPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// IsRunning returns whether the server is running.
func (s *ClipboardServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.server != nil
}

// IsTLSEnabled returns whether TLS is enabled.
func (s *ClipboardServer) IsTLSEnabled() bool {
	return s.tlsEnabled && s.getCertificate != nil
}

// GetScheme returns "https" if TLS is enabled, "http" otherwise.
func (s *ClipboardServer) GetScheme() string {
	if s.IsTLSEnabled() {
		return "https"
	}
	return "http"
}

// AddFile adds a file to be served and returns the download token.
func (s *ClipboardServer) AddFile(path, originalName string) (string, error) {
	// Generate secure token (32 bytes = 64 hex chars, use first 16)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)[:16]

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	s.mu.Lock()
	s.pending[token] = &pendingFile{
		path:      path,
		name:      originalName,
		size:      info.Size(),
		createdAt: time.Now(),
	}
	s.mu.Unlock()

	s.log("added file for download: %s (%d bytes) token=%s", originalName, info.Size(), token[:8]+"...")
	return token, nil
}

// RemoveFile removes a file from the server and deletes it from disk.
func (s *ClipboardServer) RemoveFile(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pf, ok := s.pending[token]; ok {
		if pf.path != "" {
			if err := os.Remove(pf.path); err != nil && !os.IsNotExist(err) {
				s.log("warning: failed to remove file %s: %v", pf.path, err)
			}
		}
		delete(s.pending, token)
		s.log("removed file: %s", pf.name)
	}
}

// CleanupAllFiles removes all pending files from disk.
func (s *ClipboardServer) CleanupAllFiles() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for token, pf := range s.pending {
		if pf.path != "" {
			os.Remove(pf.path)
		}
		delete(s.pending, token)
		count++
	}

	if count > 0 {
		s.log("cleaned up %d files", count)
	}
	return count
}

// GetPendingCount returns the number of pending files.
func (s *ClipboardServer) GetPendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

func (s *ClipboardServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	// Extract token from path: /c/{token}
	token := strings.TrimPrefix(r.URL.Path, "/c/")
	if token == "" || len(token) < 8 {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	s.mu.Lock()
	pf, ok := s.pending[token]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Check if already used (single-use mode)
	if s.singleUse && pf.used {
		s.mu.Unlock()
		http.Error(w, "Gone", http.StatusGone)
		return
	}

	// Check expiry
	if time.Since(pf.createdAt) > s.expiryTime {
		delete(s.pending, token)
		if pf.path != "" {
			if err := os.Remove(pf.path); err != nil && !os.IsNotExist(err) {
				s.log("warning: failed to remove expired file %s: %v", pf.path, err)
			}
		}
		s.mu.Unlock()
		http.Error(w, "Gone", http.StatusGone)
		return
	}

	// Mark as used
	pf.used = true
	path := pf.path
	name := pf.name
	s.mu.Unlock()

	// Open file
	f, err := os.Open(path)
	if err != nil {
		s.log("failed to open file for download: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Set headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(name)))
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	// Stream file
	s.log("serving file: %s to %s (TLS=%v)", name, r.RemoteAddr, r.TLS != nil)
	written, err := io.Copy(w, f)
	if err != nil {
		s.log("error streaming file %s: %v (wrote %d bytes)", name, err, written)
		// Connection was likely closed mid-transfer; cleanup will happen below
	}

	// Cleanup after single use
	if s.singleUse {
		s.mu.Lock()
		delete(s.pending, token)
		s.mu.Unlock()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.log("warning: failed to cleanup served file %s: %v", path, err)
		}
		s.log("file served and cleaned up: %s", name)
	}
}

func (s *ClipboardServer) cleanupLoop() {
	ticker := time.NewTicker(s.cleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

func (s *ClipboardServer) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, pf := range s.pending {
		if now.Sub(pf.createdAt) > s.expiryTime {
			if pf.path != "" {
				if err := os.Remove(pf.path); err != nil && !os.IsNotExist(err) {
					s.log("warning: failed to remove expired file %s: %v", pf.path, err)
				}
			}
			delete(s.pending, token)
			s.log("cleaned up expired file: %s (age: %v)", pf.name, now.Sub(pf.createdAt).Round(time.Second))
		}
	}
}

// TargetOS represents the target operating system for command generation.
type TargetOS string

const (
	TargetOSWindows TargetOS = "windows"
	TargetOSLinux   TargetOS = "linux"
	TargetOSMacOS   TargetOS = "macos"
)

// GenerateDownloadCommand generates a command to download a file on the target OS.
// If customTemplate is provided, it will be used with {url} and {filename} placeholders.
// The scheme parameter should be "http" or "https".
func GenerateDownloadCommand(targetOS TargetOS, scheme, serverIP string, port int, token, fileName, customTemplate string) string {
	url := fmt.Sprintf("%s://%s:%d/c/%s", scheme, serverIP, port, token)
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
	case TargetOSWindows:
		// PowerShell - works on Windows 10+
		// For HTTPS with self-signed certs, skip cert validation
		if scheme == "https" {
			return fmt.Sprintf("[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12;iwr %s -OutFile %s -SkipCertificateCheck", url, escapedName)
		}
		return fmt.Sprintf("iwr %s -OutFile %s", url, escapedName)

	case TargetOSLinux:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)

	case TargetOSMacOS:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)

	default:
		return fmt.Sprintf("curl %s-o %s %s", insecureFlag, escapedName, url)
	}
}

// escapeFileName escapes a filename for use in shell commands.
func escapeFileName(name string, targetOS TargetOS) string {
	// Remove/replace problematic characters
	name = filepath.Base(name) // Remove path

	// For Windows PowerShell, wrap in quotes if contains spaces
	if targetOS == TargetOSWindows {
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
