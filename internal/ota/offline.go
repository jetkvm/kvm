package ota

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
)

// OfflineBundle represents a validated offline update archive that has been
// extracted and is ready for verification.
type OfflineBundle struct {
	BinaryPath    string // absolute path to the extracted binary
	ExpectedHash  string // SHA256 hex digest read from the .sha256 file
	Signature     []byte // raw GPG signature bytes
	PublicKeyData []byte // armored GPG public key bytes
	Component     string // "app" or "system"
}

// OfflineVerifyResult captures the outcome of offline bundle verification.
type OfflineVerifyResult struct {
	HashOK      bool   `json:"hashOK"`
	SignatureOK bool   `json:"signatureOK"`
}

// expectedBinaryNames maps component names to the binary filename expected
// inside the offline update archive.
var expectedBinaryNames = map[string]string{
	"app":    "jetkvm_app",
	"system": "update_system.tar",
}

// ExtractOfflineArchive reads a gzipped tar archive from r and extracts it
// into destDir. It validates the archive structure: exactly one binary
// matching the component, one .sha256 hash file, and optionally one .sig
// file. Path traversal attempts are rejected.
func ExtractOfflineArchive(r io.Reader, destDir string, component string, l *zerolog.Logger) (*OfflineBundle, error) {
	binaryName, ok := expectedBinaryNames[component]
	if !ok {
		return nil, fmt.Errorf("unknown component: %s", component)
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	bundle := &OfflineBundle{Component: component}
	fileCount := 0
	const maxFiles = 4 // binary + .sha256 + .sig + .pub

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading tar: %w", err)
		}

		name := filepath.Clean(header.Name)

		// strip leading directory component if the archive was created with one
		name = filepath.Base(name)

		if strings.Contains(name, "..") || filepath.IsAbs(name) {
			return nil, fmt.Errorf("path traversal detected in archive: %s", header.Name)
		}

		// skip directories
		if header.Typeflag == tar.TypeDir {
			continue
		}

		fileCount++
		if fileCount > maxFiles {
			return nil, fmt.Errorf("archive contains more than %d files", maxFiles)
		}

		destPath := filepath.Join(destDir, name)

		switch {
		case name == binaryName:
			if err := extractFileFromTar(tr, destPath, header.Mode); err != nil {
				return nil, fmt.Errorf("error extracting binary: %w", err)
			}
			bundle.BinaryPath = destPath
			l.Debug().Str("path", destPath).Msg("extracted binary")

		case name == binaryName+".sha256":
			hashBytes, err := io.ReadAll(io.LimitReader(tr, 256))
			if err != nil {
				return nil, fmt.Errorf("error reading hash file: %w", err)
			}
			// hash file format: "<hex> <filename>" or just "<hex>"
			hashStr := strings.TrimSpace(string(hashBytes))
			if idx := strings.IndexByte(hashStr, ' '); idx > 0 {
				hashStr = hashStr[:idx]
			}
			bundle.ExpectedHash = strings.ToLower(hashStr)
			l.Debug().Str("hash", bundle.ExpectedHash).Msg("read expected hash")

		case name == binaryName+".sig":
			sig, err := io.ReadAll(io.LimitReader(tr, 8192))
			if err != nil {
				return nil, fmt.Errorf("error reading signature file: %w", err)
			}
			bundle.Signature = sig
			l.Debug().Int("bytes", len(sig)).Msg("read signature")

		case name == binaryName+".pub":
			pub, err := io.ReadAll(io.LimitReader(tr, 65536))
			if err != nil {
				return nil, fmt.Errorf("error reading public key file: %w", err)
			}
			bundle.PublicKeyData = pub
			l.Debug().Int("bytes", len(pub)).Msg("read public key")

		default:
			return nil, fmt.Errorf("unexpected file in archive: %s", name)
		}
	}

	if bundle.BinaryPath == "" {
		return nil, fmt.Errorf("archive missing required binary: %s", binaryName)
	}
	if bundle.ExpectedHash == "" {
		return nil, fmt.Errorf("archive missing required hash file: %s.sha256", binaryName)
	}
	if len(bundle.Signature) == 0 {
		return nil, fmt.Errorf("archive missing required signature file: %s.sig", binaryName)
	}
	if len(bundle.PublicKeyData) == 0 {
		return nil, fmt.Errorf("archive missing required public key file: %s.pub", binaryName)
	}

	return bundle, nil
}

// extractFileFromTar writes a tar entry to the given destination path.
func extractFileFromTar(tr *tar.Reader, destPath string, mode int64) error {
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode)|0644)
	if err != nil {
		return fmt.Errorf("error creating file %s: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, tr); err != nil {
		return fmt.Errorf("error writing file %s: %w", destPath, err)
	}
	return nil
}

// VerifyOfflineBundle checks the SHA256 hash and GPG signature of an
// extracted offline bundle. The bundled public key is validated against the
// pinned root fingerprint before use — no keyserver access is required.
// Hash mismatches and signature failures are always fatal.
func VerifyOfflineBundle(bundle *OfflineBundle, gpgVerifier *GPGVerifier, l *zerolog.Logger) (*OfflineVerifyResult, error) {
	result := &OfflineVerifyResult{}

	// SHA256 verification
	hash, err := hashFile(bundle.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("error hashing file: %w", err)
	}

	if hash != bundle.ExpectedHash {
		return nil, fmt.Errorf("hash mismatch: got %s, expected %s", hash, bundle.ExpectedHash)
	}
	result.HashOK = true
	l.Info().Str("hash", hash).Msg("SHA256 hash verified")

	// GPG signature verification using the bundled public key
	if len(bundle.Signature) == 0 {
		return nil, fmt.Errorf("signature is required for offline updates")
	}
	if len(bundle.PublicKeyData) == 0 {
		return nil, fmt.Errorf("public key is required for offline updates")
	}

	if err := gpgVerifier.VerifySignatureFromFileWithKey(bundle.Signature, bundle.BinaryPath, bundle.PublicKeyData); err != nil {
		return nil, fmt.Errorf("GPG signature verification failed: %w", err)
	}

	result.SignatureOK = true
	l.Info().Msg("GPG signature verified")
	return result, nil
}

// hashFile computes the SHA256 hex digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
