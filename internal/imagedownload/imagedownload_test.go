package imagedownload

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var payload = bytes.Repeat([]byte("jetkvm-image-"), 40_000)

type recorder struct {
	mu     sync.Mutex
	states []State
}

func (r *recorder) add(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *recorder) all() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]State(nil), r.states...)
}

func newDownloader(t *testing.T) (*Downloader, *recorder) {
	t.Helper()
	rec := &recorder{}
	client := NewClient(http.DefaultTransport.(*http.Transport))
	d := &Downloader{
		Dir:       t.TempDir(),
		Client:    func() *http.Client { return client },
		FreeSpace: func() (int64, error) { return 1 << 40, nil },
		OnChange:  rec.add,
	}
	t.Cleanup(func() {
		d.Cancel()
		d.Wait()
	})
	return d, rec
}

func run(t *testing.T, d *Downloader, url, filename string) State {
	t.Helper()
	if _, err := d.Start(url, filename); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d.Wait()
	return d.State()
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Fatalf("expected no files, found %v", names)
	}
}

func requireFailed(t *testing.T, s State, want error) {
	t.Helper()
	if s.Status != StatusFailed || s.Error != want.Error() {
		t.Fatalf("state = %+v, want failed with %q", s, want)
	}
}

func partialSender(release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Write(payload[:1000])
		w.(http.Flusher).Flush()
		select {
		case <-release:
			w.Write(payload[1000:])
		case <-r.Context().Done():
		}
	}
}

func waitReceived(t *testing.T, d *Downloader) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for d.State().Received == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no data received")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDownloadStoresFile(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run("chunked="+strconv.FormatBool(chunked), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chunked {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				}
				w.Write(payload)
			}))
			defer ts.Close()

			d, rec := newDownloader(t)
			s := run(t, d, ts.URL+"/dir/Some%20Image.ISO?token=abc", "")

			wantSize := int64(len(payload))
			if chunked {
				wantSize = 0
			}
			want := State{Status: StatusDone, Filename: "Some Image.ISO", Size: wantSize, Received: int64(len(payload))}
			if s != want {
				t.Fatalf("state = %+v, want %+v", s, want)
			}
			got, err := os.ReadFile(filepath.Join(d.Dir, "Some Image.ISO"))
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("stored file differs from the payload (err %v, %d bytes)", err, len(got))
			}
			if names := dirEntries(t, d.Dir); len(names) != 1 {
				t.Fatalf("unexpected files %v", names)
			}
			states := rec.all()
			if states[0].Status != StatusDownloading || states[len(states)-1] != want {
				t.Fatalf("events = %+v", states)
			}
		})
	}
}

func TestDownloadHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	d, _ := newDownloader(t)
	s := run(t, d, ts.URL+"/missing.iso", "")
	if s.Status != StatusFailed || !strings.Contains(s.Error, "404") {
		t.Fatalf("state = %+v", s)
	}
	requireEmptyDir(t, d.Dir)
}

func TestDownloadStalledServer(t *testing.T) {
	ts := httptest.NewServer(partialSender(nil))
	defer ts.Close()

	d, _ := newDownloader(t)
	d.StallTimeout = 200 * time.Millisecond
	start := time.Now()
	s := run(t, d, ts.URL+"/a.iso", "")
	requireFailed(t, s, ErrStalled)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stall detected after %v", elapsed)
	}
	requireEmptyDir(t, d.Dir)
}

func TestDownloadLargerThanFreeSpace(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Write(payload)
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	d.FreeSpace = func() (int64, error) { return int64(len(payload)) - 1, nil }
	requireFailed(t, run(t, d, ts.URL+"/a.iso", ""), ErrNoSpace)
	requireEmptyDir(t, d.Dir)

	d.FreeSpace = func() (int64, error) { return int64(len(payload)), nil }
	if s := run(t, d, ts.URL+"/a.iso", ""); s.Status != StatusDone {
		t.Fatalf("a file that fits exactly was refused: %+v", s)
	}
}

func TestCancelStopsTransferAndRemovesPartial(t *testing.T) {
	ts := httptest.NewServer(partialSender(nil))
	defer ts.Close()

	d, _ := newDownloader(t)
	if _, err := d.Start(ts.URL+"/a.iso", ""); err != nil {
		t.Fatal(err)
	}
	waitReceived(t, d)
	if names := dirEntries(t, d.Dir); len(names) != 1 || names[0] != "a.iso"+IncompleteSuffix {
		t.Fatalf("partial file during transfer = %v", names)
	}

	start := time.Now()
	d.Cancel()
	d.Wait()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
	if s := d.State(); s.Status != StatusCancelled || s.Error != "" {
		t.Fatalf("state = %+v", s)
	}
	requireEmptyDir(t, d.Dir)
}

func TestCancelBeforeResponseHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	if _, err := d.Start(ts.URL+"/a.iso", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	d.Cancel()
	d.Wait()
	if s := d.State(); s.Status != StatusCancelled {
		t.Fatalf("state = %+v", s)
	}
	requireEmptyDir(t, d.Dir)
}

func TestRedirectIsFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old.iso", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mirror/new.iso", http.StatusFound)
	})
	mux.HandleFunc("/mirror/new.iso", func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	d, _ := newDownloader(t)
	if s := run(t, d, ts.URL+"/old.iso", ""); s.Status != StatusDone {
		t.Fatalf("state = %+v", s)
	}
	got, _ := os.ReadFile(filepath.Join(d.Dir, "old.iso"))
	if !bytes.Equal(got, payload) {
		t.Fatal("redirected download stored the wrong content")
	}
}

func TestRedirectFromHTTPSToHTTPIsRefused(t *testing.T) {
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits.Add(1)
		w.Write(payload)
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/a.iso", http.StatusFound)
	}))
	defer secure.Close()

	d, _ := newDownloader(t)
	client := NewClient(secure.Client().Transport.(*http.Transport))
	d.Client = func() *http.Client { return client }
	requireFailed(t, run(t, d, secure.URL+"/a.iso", ""), ErrDowngrade)
	if plainHits.Load() != 0 {
		t.Fatal("the HTTP target of the redirect was requested")
	}
	requireEmptyDir(t, d.Dir)
}

func TestRedirectLoopEnds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	requireFailed(t, run(t, d, ts.URL+"/a.iso", ""), ErrTooManyRedirects)
}

func TestStartRejectsBadInputWithoutRequest(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(payload)
	}))
	defer ts.Close()

	parent := t.TempDir()
	d, _ := newDownloader(t)
	d.Dir = filepath.Join(parent, "images")
	if err := os.Mkdir(d.Dir, 0755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		url, filename string
		want          error
	}{
		{ts.URL + "/a.iso", "../escape.iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", "sub/a.iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", `sub\a.iso`, ErrInvalidFilename},
		{ts.URL + "/a.iso", "/etc/a.iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", ".hidden.iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", "new\nline.iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", strings.Repeat("a", 250) + ".iso", ErrInvalidFilename},
		{ts.URL + "/a.iso", "a.iso" + IncompleteSuffix, ErrExtension},
		{ts.URL + "/a.iso", "a.sh", ErrExtension},
		{ts.URL + "/download", "", ErrExtension},
		{ts.URL + "/", "", ErrInvalidFilename},
		{ts.URL, "", ErrInvalidFilename},
		{ts.URL + "/..%2F..%2Fescape.iso", "", nil},
		{"ftp://example.com/a.iso", "", ErrUnsupportedScheme},
		{"file:///etc/passwd", "a.iso", ErrInvalidURL},
		{"example.com/a.iso", "", ErrInvalidURL},
		{"", "a.iso", ErrInvalidURL},
	}
	for _, c := range cases {
		_, err := d.Start(c.url, c.filename)
		if !errors.Is(err, c.want) {
			t.Errorf("Start(%q, %q) = %v, want %v", c.url, c.filename, err, c.want)
		}
		d.Wait()
	}

	if names := dirEntries(t, parent); len(names) != 1 {
		t.Fatalf("files outside the images folder: %v", names)
	}
	if names := dirEntries(t, d.Dir); len(names) != 1 || names[0] != "escape.iso" {
		t.Fatalf("images folder = %v, want only the encoded-slash URL stored as escape.iso", names)
	}
	if hits.Load() != 1 {
		t.Fatalf("server saw %d requests, want 1", hits.Load())
	}
}

func TestSecondDownloadIsRefusedWhileOneRuns(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(partialSender(release))
	defer ts.Close()

	d, _ := newDownloader(t)
	if _, err := d.Start(ts.URL+"/first.iso", ""); err != nil {
		t.Fatal(err)
	}
	waitReceived(t, d)
	if _, err := d.Start(ts.URL+"/second.iso", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start = %v, want ErrBusy", err)
	}
	if name := d.ActiveFilename(); name != "first.iso" {
		t.Fatalf("active file = %q", name)
	}

	close(release)
	d.Wait()
	if s := d.State(); s.Status != StatusDone || s.Filename != "first.iso" {
		t.Fatalf("first download = %+v", s)
	}
	if names := dirEntries(t, d.Dir); len(names) != 1 || names[0] != "first.iso" {
		t.Fatalf("files = %v", names)
	}
	if name := d.ActiveFilename(); name != "" {
		t.Fatalf("active file after the end = %q", name)
	}
}

func TestExistingFileIsNotOverwritten(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(payload)
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	existing := filepath.Join(d.Dir, "a.iso")
	if err := os.WriteFile(existing, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Start(ts.URL+"/a.iso", ""); !errors.Is(err, ErrExists) {
		t.Fatalf("Start = %v, want ErrExists", err)
	}
	if hits.Load() != 0 {
		t.Fatal("a request was sent for a file name that is taken")
	}
	if got, _ := os.ReadFile(existing); string(got) != "keep" {
		t.Fatalf("existing file changed to %d bytes", len(got))
	}
}

func TestFileCreatedDuringDownloadIsNotOverwritten(t *testing.T) {
	var target string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		os.WriteFile(target, []byte("keep"), 0644)
		w.Write(payload)
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	target = filepath.Join(d.Dir, "a.iso")
	requireFailed(t, run(t, d, ts.URL+"/a.iso", ""), ErrExists)
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("existing file changed to %d bytes", len(got))
	}
	if names := dirEntries(t, d.Dir); len(names) != 1 {
		t.Fatalf("files = %v", names)
	}
}

func TestTruncatedBodyLeavesNoFile(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Write(payload[:len(payload)/2])
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	if s := run(t, d, ts.URL+"/a.iso", ""); s.Status != StatusFailed {
		t.Fatalf("state = %+v", s)
	}
	requireEmptyDir(t, d.Dir)
}

func TestStalePartialIsReplaced(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Write(payload)
	}))
	defer ts.Close()

	d, _ := newDownloader(t)
	stale := bytes.Repeat([]byte("x"), len(payload)*2)
	if err := os.WriteFile(filepath.Join(d.Dir, "a.iso"+IncompleteSuffix), stale, 0644); err != nil {
		t.Fatal(err)
	}
	capacity := int64(len(stale)) + 10
	d.FreeSpace = func() (int64, error) {
		used := int64(0)
		entries, _ := os.ReadDir(d.Dir)
		for _, e := range entries {
			info, _ := e.Info()
			used += info.Size()
		}
		return capacity - used, nil
	}
	if s := run(t, d, ts.URL+"/a.iso", ""); s.Status != StatusDone {
		t.Fatalf("state = %+v", s)
	}
	got, _ := os.ReadFile(filepath.Join(d.Dir, "a.iso"))
	if !bytes.Equal(got, payload) {
		t.Fatalf("stored %d bytes, want the %d-byte payload", len(got), len(payload))
	}
}

func TestErrorDoesNotContainURL(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()

	d, _ := newDownloader(t)
	s := run(t, d, strings.Replace(addr, "http://", "http://user:hunter2@", 1)+"/a.iso?token=s3cret", "")
	if s.Status != StatusFailed || s.Error == "" {
		t.Fatalf("state = %+v", s)
	}
	for _, secret := range []string{"s3cret", "hunter2", "a.iso"} {
		if strings.Contains(s.Error, secret) {
			t.Fatalf("error %q contains %q", s.Error, secret)
		}
	}
}

func TestProgressEventsAreRateLimited(t *testing.T) {
	const chunks = 100
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(chunks*1000))
		for i := 0; i < chunks; i++ {
			w.Write(payload[:1000])
			w.(http.Flusher).Flush()
			time.Sleep(3 * time.Millisecond)
		}
	}))
	defer ts.Close()

	d, rec := newDownloader(t)
	d.NotifyInterval = 100 * time.Millisecond
	start := time.Now()
	run(t, d, ts.URL+"/a.iso", "")
	elapsed := time.Since(start)

	states := rec.all()
	limit := int(elapsed/d.NotifyInterval) + 2
	if len(states) > limit {
		t.Fatalf("%d events in %v, limit %d", len(states), elapsed, limit)
	}
	var previous int64 = -1
	for _, s := range states {
		if s.Received < previous {
			t.Fatalf("progress went backwards: %+v", states)
		}
		previous = s.Received
	}
	if last := states[len(states)-1]; last.Status != StatusDone || last.Received != chunks*1000 {
		t.Fatalf("last event = %+v", last)
	}
}
