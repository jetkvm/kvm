package kvm

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jetkvm/kvm/internal/ota"
)

const (
	// maxOfflineUploadSize limits offline update archives to 200MB.
	maxOfflineUploadSize = 200 << 20
)

// offlineUpdateUploadResponse is returned by POST /ota/upload.
type offlineUpdateUploadResponse struct {
	Verified    bool   `json:"verified"`
	HashOK      bool   `json:"hashOK"`
	SignatureOK bool   `json:"signatureOK"`
	Error       string `json:"error,omitempty"`
}

// handleOfflineUpdateUpload handles POST /ota/upload.
// Accepts a multipart form with fields:
//   - component: "app" or "system"
//   - file: the .tar.gz offline update archive
//
// Extracts, validates structure, verifies hash + GPG signature, and stages
// the verified binary at the standard OTA path.
func handleOfflineUpdateUpload(c *gin.Context) {
	if otaState.IsUpdatePending() {
		c.JSON(http.StatusConflict, offlineUpdateUploadResponse{
			Error: "an update is already in progress",
		})
		return
	}

	component := c.PostForm("component")
	if component != "app" && component != "system" {
		c.JSON(http.StatusBadRequest, offlineUpdateUploadResponse{
			Error: "component must be 'app' or 'system'",
		})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, offlineUpdateUploadResponse{
			Error: "missing or invalid file upload",
		})
		return
	}

	if file.Size > maxOfflineUploadSize {
		c.JSON(http.StatusRequestEntityTooLarge, offlineUpdateUploadResponse{
			Error: fmt.Sprintf("file exceeds maximum size of %d MB", maxOfflineUploadSize>>20),
		})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, offlineUpdateUploadResponse{
			Error: "failed to read uploaded file",
		})
		return
	}
	defer f.Close()

	// Extract to a temp directory
	extractDir, err := os.MkdirTemp("", "jetkvm-offline-update-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, offlineUpdateUploadResponse{
			Error: "failed to create temporary directory",
		})
		return
	}
	defer os.RemoveAll(extractDir)

	l := otaLogger.With().Str("component", component).Logger()

	bundle, err := ota.ExtractOfflineArchive(f, extractDir, component, &l)
	if err != nil {
		l.Warn().Err(err).Msg("offline archive extraction failed")
		c.JSON(http.StatusBadRequest, offlineUpdateUploadResponse{
			Error: fmt.Sprintf("invalid archive: %v", err),
		})
		return
	}

	result, err := ota.VerifyOfflineBundle(bundle, otaState.GPGVerifier(), &l)
	if err != nil {
		l.Warn().Err(err).Msg("offline bundle verification failed")
		c.JSON(http.StatusUnprocessableEntity, offlineUpdateUploadResponse{
			Error: fmt.Sprintf("verification failed: %v", err),
		})
		return
	}

	// Stage the verified binary at the standard OTA path
	destPath, err := ota.ComponentUpdatePath(component)
	if err != nil {
		c.JSON(http.StatusInternalServerError, offlineUpdateUploadResponse{
			Error: fmt.Sprintf("internal error: %v", err),
		})
		return
	}

	if err := os.Rename(bundle.BinaryPath, destPath); err != nil {
		// Rename may fail across filesystems; fall back to copy
		if err := copyFile(bundle.BinaryPath, destPath); err != nil {
			l.Error().Err(err).Msg("failed to stage verified binary")
			c.JSON(http.StatusInternalServerError, offlineUpdateUploadResponse{
				Error: "failed to stage verified update file",
			})
			return
		}
	}

	if err := os.Chmod(destPath, 0755); err != nil {
		l.Warn().Err(err).Msg("failed to set permissions on staged file")
	}

	l.Info().
		Bool("hashOK", result.HashOK).
		Bool("signatureOK", result.SignatureOK).
		Msg("offline update uploaded and verified")

	c.JSON(http.StatusOK, offlineUpdateUploadResponse{
		Verified:    result.HashOK && result.SignatureOK,
		HashOK:      result.HashOK,
		SignatureOK: result.SignatureOK,
	})
}

// offlineUpdateApplyRequest is the body for POST /ota/apply.
type offlineUpdateApplyRequest struct {
	Component string `json:"component" binding:"required"`
}

// handleOfflineUpdateApply handles POST /ota/apply.
// Applies a previously uploaded and verified offline update.
func handleOfflineUpdateApply(c *gin.Context) {
	var req offlineUpdateApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Component != "app" && req.Component != "system" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "component must be 'app' or 'system'"})
		return
	}

	// Verify the staged file exists
	destPath, err := ota.ComponentUpdatePath(req.Component)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("internal error: %v", err)})
		return
	}

	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no staged update found; upload first"})
		return
	}

	l := otaLogger.With().Str("component", req.Component).Logger()
	l.Info().Msg("applying offline update")

	// Apply asynchronously — the device will reboot
	go func() {
		if err := otaState.ApplyOfflineUpdate(context.Background(), req.Component); err != nil {
			l.Error().Err(err).Msg("offline update apply failed")
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "update is being applied; device will reboot"})
}

// copyFile copies src to dst, used when os.Rename fails across filesystems.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}
