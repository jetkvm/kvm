package storage

import (
	"errors"
	"path/filepath"
	"strings"
)

func SanitizeFilename(filename string) (string, error) {
	cleanPath := filepath.Clean(filename)
	if filepath.IsAbs(cleanPath) || strings.Contains(cleanPath, "..") {
		return "", errors.New("invalid filename")
	}
	sanitized := filepath.Base(cleanPath)
	if sanitized == "." || sanitized == string(filepath.Separator) {
		return "", errors.New("invalid filename")
	}
	return sanitized, nil
}
