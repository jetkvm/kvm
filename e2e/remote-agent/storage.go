package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type usbMedium struct {
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Serial  string `json:"serial"`
	Size    int64  `json:"size"`
}

// Require a unique whole disk matching the device's USB identity and image size.
func findMedium(root string, identity usbMedium) (string, error) {
	blocks, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	match := ""
	for _, block := range blocks {
		path := filepath.Join(root, block.Name())
		if _, err := os.Stat(filepath.Join(path, "partition")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		for parent := filepath.Dir(resolved); parent != "/"; parent = filepath.Dir(parent) {
			vendor, err := os.ReadFile(filepath.Join(parent, "idVendor"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			product, err := os.ReadFile(filepath.Join(parent, "idProduct"))
			if err != nil {
				return "", err
			}
			serial, err := os.ReadFile(filepath.Join(parent, "serial"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			if strings.ToLower(strings.TrimSpace(string(vendor))) != identity.Vendor ||
				strings.ToLower(strings.TrimSpace(string(product))) != identity.Product ||
				(identity.Serial != "" && strings.TrimSpace(string(serial)) != identity.Serial) {
				break
			}
			data, err := os.ReadFile(filepath.Join(path, "size"))
			if err != nil {
				return "", err
			}
			sectors, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil {
				return "", err
			}
			if identity.Size%512 == 0 && sectors == identity.Size/512 {
				if match != "" {
					return "", fmt.Errorf("ambiguous USB medium: %s and %s", match, block.Name())
				}
				match = block.Name()
			}
			break
		}
	}
	if match == "" {
		return "", os.ErrNotExist
	}
	return match, nil
}

func openMedium(identity usbMedium) (*os.File, error) {
	name, err := findMedium("/sys/class/block", identity)
	if err != nil {
		return nil, err
	}
	source, err := os.Open(filepath.Join("/dev", name))
	if err != nil {
		return nil, err
	}
	info, err := source.Stat()
	if err == nil && info.Mode()&os.ModeType != os.ModeDevice {
		err = errors.New("selected path is not a block device")
	}
	if err != nil {
		source.Close()
		return nil, err
	}
	return source, nil
}

func waitForMedium(ctx context.Context, open func() (*os.File, error)) (*os.File, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := open()
		if err == nil {
			return source, nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENODEV) && !errors.Is(err, syscall.ENXIO) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("USB medium did not enumerate: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func hashMedium(source io.Reader, size int64) (string, error) {
	digest := sha256.New()
	if _, err := io.CopyN(digest, source, size); err != nil {
		return "", fmt.Errorf("USB readback incomplete: %w", err)
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func handleStorageReadback(w http.ResponseWriter, r *http.Request) {
	var identity usbMedium
	hexID := regexp.MustCompile(`^[0-9a-f]{4}$`)
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&identity); err != nil ||
		!hexID.MatchString(identity.Vendor) || !hexID.MatchString(identity.Product) || identity.Size <= 0 || identity.Size > 1<<30 {
		http.Error(w, "expected USB vendor/product, serial and image size (1 byte to 1 GiB)", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	enumeration, stopWaiting := context.WithTimeout(ctx, 25*time.Second)
	source, err := waitForMedium(enumeration, func() (*os.File, error) { return openMedium(identity) })
	stopWaiting()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer source.Close()
	stop := context.AfterFunc(ctx, func() { source.Close() })
	defer stop()
	digest, err := hashMedium(source, identity.Size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"device": source.Name(), "bytes": identity.Size, "sha256": digest})
}
