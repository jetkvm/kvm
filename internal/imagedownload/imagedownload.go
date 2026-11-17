package imagedownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	IncompleteSuffix = ".incomplete"

	connectTimeout        = 15 * time.Second
	responseHeaderTimeout = 30 * time.Second
	defaultStallTimeout   = 60 * time.Second
	defaultNotifyInterval = 500 * time.Millisecond
	maxRedirects          = 10
	maxFilenameLength     = 255 - len(IncompleteSuffix)
)

var (
	ErrInvalidURL        = errors.New("enter a valid HTTP or HTTPS URL")
	ErrUnsupportedScheme = errors.New("only HTTP and HTTPS URLs are supported")
	ErrInvalidFilename   = errors.New("invalid file name")
	ErrExtension         = errors.New("the file name must end in .iso or .img")
	ErrBusy              = errors.New("another download is already in progress")
	ErrExists            = errors.New("a file with this name already exists")
	ErrNoSpace           = errors.New("not enough free space on the device")
	ErrStalled           = errors.New("the server stopped sending data")
	ErrCancelled         = errors.New("download cancelled")
	ErrDowngrade         = errors.New("redirect from HTTPS to HTTP refused")
	ErrTooManyRedirects  = errors.New("too many redirects")
)

type Status string

const (
	StatusIdle        Status = "idle"
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
)

type State struct {
	Status   Status `json:"status"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size"`
	Received int64  `json:"received"`
	Error    string `json:"error,omitempty"`
}

func ParseURL(rawURL string) (*neturl.URL, error) {
	u, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, ErrUnsupportedScheme
	}
	return u, nil
}

func ValidateFilename(name string) error {
	if name == "" || len(name) > maxFilenameLength || strings.HasPrefix(name, ".") {
		return ErrInvalidFilename
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return ErrInvalidFilename
		}
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".iso", ".img":
		return nil
	}
	return ErrExtension
}

func NewClient(base *http.Transport) *http.Client {
	transport := base.Clone()
	transport.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return ErrTooManyRedirects
			}
			if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return ErrDowngrade
			}
			return nil
		},
	}
}

type Downloader struct {
	Dir            string
	Client         func() *http.Client
	FreeSpace      func() (int64, error)
	OnChange       func(State)
	StallTimeout   time.Duration
	NotifyInterval time.Duration

	mu     sync.Mutex
	state  State
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func (d *Downloader) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status == "" {
		return State{Status: StatusIdle}
	}
	return d.state
}

func (d *Downloader) ActiveFilename() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status != StatusDownloading {
		return ""
	}
	return d.state.Filename
}

func ResolveFilename(rawURL string, filename string) (string, error) {
	u, err := ParseURL(rawURL)
	if err != nil {
		return "", err
	}
	if filename == "" {
		filename = path.Base(u.Path)
	}
	return filename, ValidateFilename(filename)
}

func (d *Downloader) Start(rawURL string, filename string) (State, error) {
	filename, err := ResolveFilename(rawURL, filename)
	if err != nil {
		return State{}, err
	}
	u, _ := ParseURL(rawURL)

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status == StatusDownloading {
		return State{}, ErrBusy
	}
	if _, err := os.Lstat(filepath.Join(d.Dir, filename)); err == nil {
		return State{}, ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, fmt.Errorf("failed to check the file name: %w", err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	d.cancel = cancel
	d.done = make(chan struct{})
	d.state = State{Status: StatusDownloading, Filename: filename}
	go d.run(ctx, u, filename, d.done)
	return d.state, nil
}

func (d *Downloader) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status == StatusDownloading {
		d.cancel(ErrCancelled)
	}
}

func (d *Downloader) Wait() {
	d.mu.Lock()
	done := d.done
	d.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (d *Downloader) run(ctx context.Context, u *neturl.URL, filename string, done chan struct{}) {
	defer close(done)
	d.notify()

	err := d.transfer(ctx, u, filename)

	d.mu.Lock()
	d.cancel(nil)
	switch {
	case err == nil:
		d.state.Status = StatusDone
	case errors.Is(err, ErrCancelled):
		d.state.Status = StatusCancelled
	default:
		d.state.Status = StatusFailed
		d.state.Error = publicError(err)
	}
	d.mu.Unlock()
	d.notify()
}

func (d *Downloader) notify() {
	if d.OnChange != nil {
		d.OnChange(d.State())
	}
}

func causeOr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

func publicError(err error) string {
	var urlErr *neturl.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, syscall.ENOSPC) {
		err = ErrNoSpace
	}
	return err.Error()
}

func (d *Downloader) transfer(ctx context.Context, u *neturl.URL, filename string) (err error) {
	stallTimeout := d.StallTimeout
	if stallTimeout == 0 {
		stallTimeout = defaultStallTimeout
	}
	notifyInterval := d.NotifyInterval
	if notifyInterval == 0 {
		notifyInterval = defaultNotifyInterval
	}

	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := d.Client().Do(req)
	if err != nil {
		return causeOr(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the server answered HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	size := max(resp.ContentLength, 0)
	if size > 0 {
		free, err := d.FreeSpace()
		if err != nil {
			return fmt.Errorf("failed to read the free space: %w", err)
		}
		if size > free {
			return ErrNoSpace
		}
	}
	d.mu.Lock()
	d.state.Size = size
	d.mu.Unlock()

	finalPath := filepath.Join(d.Dir, filename)
	partialPath := finalPath + IncompleteSuffix
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create the file: %w", err)
	}
	defer func() {
		file.Close()
		if err != nil {
			os.Remove(partialPath)
		}
	}()

	stall := time.AfterFunc(stallTimeout, func() { stop(ErrStalled) })
	defer stall.Stop()

	buf := make([]byte, 64*1024)
	lastNotify := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			if _, err := file.Write(buf[:n]); err != nil {
				return err
			}
			d.mu.Lock()
			d.state.Received += int64(n)
			d.mu.Unlock()
			if time.Since(lastNotify) >= notifyInterval {
				lastNotify = time.Now()
				d.notify()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return causeOr(ctx, readErr)
		}
	}
	stall.Stop()

	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	err = unix.Renameat2(unix.AT_FDCWD, partialPath, unix.AT_FDCWD, finalPath, unix.RENAME_NOREPLACE)
	if errors.Is(err, syscall.EEXIST) {
		return ErrExists
	}
	return err
}
