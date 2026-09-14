package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFindMedium(t *testing.T) {
	root := t.TempDir()
	blocks := filepath.Join(root, "class")
	usb := filepath.Join(root, "usb")
	for _, path := range []string{blocks, usb} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{"idVendor": "1D6B\n", "idProduct": "0104\n", "serial": "test\n"} {
		write(filepath.Join(usb, name), value)
	}
	add := func(name string, partition bool) {
		t.Helper()
		path := filepath.Join(usb, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(path, "size"), "2\n")
		if partition {
			write(filepath.Join(path, "partition"), "1")
		}
		if err := os.Symlink(path, filepath.Join(blocks, name)); err != nil {
			t.Fatal(err)
		}
	}
	add("sda", false)
	add("sda1", true)
	identity := usbMedium{"1d6b", "0104", "test", 1024}
	if got, err := findMedium(blocks, identity); err != nil || got != "sda" {
		t.Fatalf("selection: %q, %v", got, err)
	}
	for _, wrong := range []usbMedium{{"1234", "0104", "test", 1024}, {"1d6b", "1234", "test", 1024}, {"1d6b", "0104", "other", 1024}, {"1d6b", "0104", "test", 512}} {
		if _, err := findMedium(blocks, wrong); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("matched wrong identity: %+v, %v", wrong, err)
		}
	}
	add("sdb", false)
	if _, err := findMedium(blocks, identity); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous selection: %v", err)
	}
	if err := os.Remove(filepath.Join(usb, "sdb", "size")); err != nil {
		t.Fatal(err)
	}
	if _, err := findMedium(blocks, identity); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disappearing sysfs entry: %v", err)
	}
}

func TestWaitForMediumRetriesOnlyTransientErrors(t *testing.T) {
	for _, failure := range []error{syscall.ENOENT, syscall.ENODEV, syscall.ENXIO, syscall.EACCES, syscall.EIO, errors.New("ambiguous USB medium")} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			calls := 0
			file, err := os.CreateTemp(t.TempDir(), "medium")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			got, err := waitForMedium(ctx, func() (*os.File, error) {
				calls++
				if calls == 1 {
					return nil, &os.PathError{Op: "open", Path: "/dev/test", Err: failure}
				}
				return file, nil
			})
			transient := failure == syscall.ENOENT || failure == syscall.ENODEV || failure == syscall.ENXIO
			if transient && (err != nil || got != file || calls != 2) {
				t.Fatalf("retry: calls=%d, err=%v", calls, err)
			}
			if !transient && (calls != 1 || !errors.Is(err, failure)) {
				t.Fatalf("permanent error was swallowed: %v", err)
			}
		})
	}
}

func TestWaitForMediumDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := waitForMedium(ctx, func() (*os.File, error) { return nil, syscall.ENOENT })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestHashMediumRejectsShortReads(t *testing.T) {
	if _, err := hashMedium(strings.NewReader("abc"), 4); err == nil {
		t.Fatal("short read succeeded")
	}
	got, err := hashMedium(strings.NewReader("abc-extra"), 3)
	if want := fmt.Sprintf("%x", sha256.Sum256([]byte("abc"))); err != nil || got != want {
		t.Fatalf("hash: %q, %v", got, err)
	}
}

func TestReadbackRejectsInvalidIdentity(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"vendor":"../x","product":"0104","size":512}`, `{"vendor":"1d6b","product":"0104","size":-1}`, `{"vendor":"1d6b","product":"0104","size":1073741825}`} {
		w := httptest.NewRecorder()
		handleStorageReadback(w, httptest.NewRequest("POST", "/storage/readback", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid input %s: status %d", body, w.Code)
		}
	}
}
