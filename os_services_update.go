package kvm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/osservices"
	"github.com/jetkvm/kvm/internal/osservices/release"
	"github.com/jetkvm/kvm/internal/ota"
)

// The OS Services update relay. While a sender is connected it keeps the
// newest release of the release bucket's indexes and offers it to the
// browser (guestUpdate). On guestUpdateStart, or by itself with auto-update
// on and no browser connected, it streams the sender's package from the
// bucket to the sender over the session, the last chunk only once the hash
// and the signature check out. It never runs alongside a firmware update.
// The pure parts are in internal/osservices/release.

const (
	osServicesBucketURL = "https://os-services.jetkvm.com"

	osUpdateServiceTick = 100 * time.Millisecond
	// The indexes are checked this often while a sender is connected, and
	// fetched again when older than osUpdateIndexMaxAge (a new sender
	// session: osUpdateConnectMaxAge), with their ETags.
	osUpdateCheckInterval = 300 * time.Second
	osUpdateIndexMaxAge   = time.Hour
	osUpdateConnectMaxAge = 10 * time.Minute
	// A check that never got the indexes is tried again after this, then
	// twice as long each time up to the check interval.
	osUpdateFirstRetry = 15 * time.Second
	// An automatic update that did not install waits this long.
	osUpdateAutoRetry = time.Hour
	// After Installing, the sender has this long to come back with the new
	// version.
	osUpdateRestartTimeout = 3 * time.Minute
	// A download read, and a small file's whole fetch, give up after this.
	osUpdateIdleTimeout  = 30 * time.Second
	osUpdateCreditPoll   = 5 * time.Millisecond
	osUpdateSettlePoll   = 50 * time.Millisecond
	osUpdateProgressPush = time.Second

	osUpdateUnavailable      = "the OS Services sender cannot install an update"
	osUpdateStopped          = "stopped for a firmware update"
	osUpdateFirmwareUpdating = "a firmware update is in progress"
	osUpdateInProgress       = "an OS Services update is already in progress"
	osUpdateUnreachable      = "could not reach the OS Services release server"
)

const (
	osUpdateIndexStable = iota
	osUpdateIndexDev
)

var osUpdateIndexNames = [2]string{"releases.json", "releases-dev.json"}

type osServicesUpdates struct {
	svc      *osServicesService
	verifier *ota.GPGVerifier

	mu sync.Mutex
	// The worker's jobs, and the cancel of its downloads.
	workerRunning bool
	refreshWanted bool
	refreshMaxAge time.Duration
	updateWanted  bool
	cancel        context.CancelFunc
	// A firmware update stopped the worker's job.
	stop bool
	// The OTA settings, as the service last read them.
	automatic  bool
	includeDev bool
	// The indexes' newest releases, their ETags, and when they were read.
	releases   [2]release.Release
	etags      [2]string
	fetchedAt  time.Time
	devFetched bool
	nextCheck  time.Time
	retry      time.Duration
	// The connected sender, and the session it reported in.
	sender        release.Sender
	senderSession uint32
	// The update and the package it installs.
	progress    release.Progress
	pkg         release.Package
	pkgSigned   bool
	autoTried   string
	autoTriedAt time.Time
	// The sender session the package was chosen for; only it installs.
	updateSession      uint32
	installingDeadline time.Time
	// The session's update, once begun, and its outcome once ended.
	updateID         uint32
	ended            bool
	status           uint8
	message          string
	progressPushedAt time.Time
}

var (
	osServicesUpdatesOnce sync.Once
	osServicesUpdater     *osServicesUpdates
)

// osServicesUpdateRelay is the relay, started by the first call: the first
// state the browser reads or the session pushes (a sender's handshake
// pushes one). nil without OS Services.
func osServicesUpdateRelay() *osServicesUpdates {
	if osServices == nil {
		return nil
	}
	osServicesUpdatesOnce.Do(func() {
		r := &osServicesUpdates{
			svc: osServices,
			verifier: ota.NewGPGVerifier(osServicesLogger, func() ota.HttpClient {
				return osServicesHTTPClient()
			}),
		}
		r.service()
		osServicesUpdater = r
		go r.run()
	})
	return osServicesUpdater
}

// osServicesUpdateState is the browser's guestUpdate: the newest release
// and the update of the connected sender to it, or nil while neither is
// known.
func osServicesUpdateState(st osservices.State) any {
	r := osServicesUpdateRelay()
	if r == nil {
		return nil
	}
	r.mu.Lock()
	state := release.Render(r.newestLocked(), osUpdateSender(st), r.progress, r.automatic)
	r.mu.Unlock()
	if state == nil {
		return nil
	}
	return state
}

// rpcGuestUpdateStart installs the newest release on the sender now.
func rpcGuestUpdateStart() error {
	r := osServicesUpdateRelay()
	if r == nil {
		return errors.New(osUpdateUnavailable)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if reason := r.startLocked(false); reason != "" {
		return errors.New(reason)
	}
	return nil
}

// osUpdateSender is the connected sender as the relay sees it.
func osUpdateSender(st osservices.State) release.Sender {
	var sender release.Sender
	if st.SenderInfo {
		sender.Version = st.SenderVersion
		if st.UpdateInstall {
			sender.Package = st.UpdatePackage
		}
	}
	return sender
}

func osUpdateSettings() (automatic, includeDev bool) {
	configLock.Lock()
	defer configLock.Unlock()
	if config == nil {
		return false, false
	}
	return config.AutoUpdateEnabled, config.IncludePreRelease
}

// firmwareUpdatePending: the firmware OTA runs (it checks or installs). The
// relay leaves the network to it.
func firmwareUpdatePending() bool {
	return otaState != nil && otaState.IsUpdatePending()
}

func osServicesHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = config.NetworkConfig.GetTransportProxyFunc()
	transport.ResponseHeaderTimeout = osUpdateIdleTimeout
	return &http.Client{Transport: transport}
}

// --- Service: follows the session and the settings ------------------------------

func (r *osServicesUpdates) run() {
	t := time.NewTicker(osUpdateServiceTick)
	defer t.Stop()
	for range t.C {
		r.service()
	}
}

func (r *osServicesUpdates) service() {
	automatic, includeDev := osUpdateSettings()
	var st osservices.State
	var opened uint32
	var update osservices.Update
	r.svc.do(func(session *osservices.Session) {
		st = session.State()
		opened = session.Counters.SessionsOpened
		update = session.Update()
	})
	firmware := firmwareUpdatePending()
	r.mu.Lock()
	r.followUpdateLocked(update)
	r.followSenderLocked(osUpdateSender(st), opened)
	r.followSettingsLocked(automatic, includeDev)
	var stopID uint32
	if firmware {
		stopID = r.stopLocked()
	}
	r.mu.Unlock()
	if stopID != 0 {
		r.svc.do(func(session *osservices.Session) { session.UpdateCancel(stopID, osUpdateStopped) })
	}
}

// changedLocked: the browser should see the state again.
func (r *osServicesUpdates) changedLocked() { r.svc.StateChanged() }

func (r *osServicesUpdates) newestLocked() *release.Release {
	return release.Newest(&r.releases[osUpdateIndexStable], &r.releases[osUpdateIndexDev], r.includeDev)
}

func (r *osServicesUpdates) finishLocked(phase release.Phase, message string) {
	r.progress.Phase = phase
	r.progress.Error = message
	r.changedLocked()
}

// followUpdateLocked takes the session's update outcome and progress.
func (r *osServicesUpdates) followUpdateLocked(u osservices.Update) {
	if r.updateID == 0 || r.ended || u.UpdateID != r.updateID {
		return
	}
	if u.Ended {
		r.ended = true
		r.status = u.Status
		r.message = u.Message
		// The sender restarts; its next session reports the version, maybe
		// before the worker has closed its download.
		if u.Status == osservices.UpdateInstalling {
			r.installingDeadline = time.Now().Add(osUpdateRestartTimeout)
			r.finishLocked(release.PhaseInstalling, "")
		}
	}
	// Progress at most once a second.
	if u.Sent != r.progress.Sent && (u.Sent == u.Size || time.Since(r.progressPushedAt) >= osUpdateProgressPush) {
		r.progress.Sent = u.Sent
		r.progressPushedAt = time.Now()
		r.changedLocked()
	}
}

func (r *osServicesUpdates) followSenderLocked(sender release.Sender, opened uint32) {
	if sender != r.sender {
		r.sender = sender
		r.changedLocked()
	}
	if sender.Version != "" && r.senderSession != opened {
		r.senderSession = opened
		r.senderConnectedLocked()
	}
	if r.progress.Phase == release.PhaseInstalling && time.Now().After(r.installingDeadline) {
		r.finishLocked(release.PhaseFailed, "OS Services did not start again in 3 minutes")
	}
	if sender.Version != "" && !time.Now().Before(r.nextCheck) {
		r.requestRefreshLocked(osUpdateIndexMaxAge)
	}
}

// senderConnectedLocked: a sender reported in a new session. An update
// that was installing ends with its version, one that is done no longer
// describes another version, and the indexes are refreshed.
func (r *osServicesUpdates) senderConnectedLocked() {
	p := r.progress
	same := r.sender.Version == p.Version
	switch {
	case p.Phase == release.PhaseDone && !same:
		r.progress = release.Progress{}
		r.changedLocked()
	case (p.Phase == release.PhaseInstalling && r.senderSession != r.updateSession) ||
		(p.Phase == release.PhaseFailed && same):
		if same {
			r.finishLocked(release.PhaseDone, "")
		} else {
			r.finishLocked(release.PhaseFailed, "OS Services restarted with another version")
		}
	}
	r.requestRefreshLocked(osUpdateConnectMaxAge)
}

func (r *osServicesUpdates) followSettingsLocked(automatic, includeDev bool) {
	if automatic == r.automatic && includeDev == r.includeDev {
		return
	}
	r.automatic = automatic
	r.includeDev = includeDev
	r.changedLocked()
	if r.sender.Version != "" {
		r.requestRefreshLocked(osUpdateIndexMaxAge)
	}
}

// stopLocked: a firmware update needs the network to itself. The worker's
// downloads stop; it returns the session update to cancel, 0 for none.
func (r *osServicesUpdates) stopLocked() uint32 {
	if !r.workerRunning || r.stop {
		return 0
	}
	r.stop = true
	if r.cancel != nil {
		r.cancel()
	}
	if r.updateID == 0 || r.ended {
		return 0
	}
	return r.updateID
}

func (r *osServicesUpdates) requestRefreshLocked(maxAge time.Duration) {
	if !r.refreshWanted || maxAge < r.refreshMaxAge {
		r.refreshMaxAge = maxAge
	}
	r.refreshWanted = true
	r.nextCheck = time.Now().Add(osUpdateCheckInterval)
	r.wakeWorkerLocked()
}

// retryLocked: no check has got the indexes yet, as at boot before the
// network is up. Nothing is offered until one does, so try again soon.
func (r *osServicesUpdates) retryLocked() {
	if !r.fetchedAt.IsZero() {
		return
	}
	if r.retry == 0 {
		r.retry = osUpdateFirstRetry
	} else {
		r.retry *= 2
	}
	r.retry = min(r.retry, osUpdateCheckInterval)
	r.nextCheck = time.Now().Add(r.retry)
}

// startLocked starts an update to the newest release, or says why not.
func (r *osServicesUpdates) startLocked(automatic bool) string {
	newest := r.newestLocked()
	pkg := release.Target(newest, r.sender)
	switch {
	case pkg == nil:
		return osUpdateUnavailable
	case r.progress.Running():
		return osUpdateInProgress
	case firmwareUpdatePending():
		return osUpdateFirmwareUpdating
	}
	r.pkg = *pkg
	r.pkgSigned = newest.Signed
	r.updateSession = r.senderSession
	r.progress = release.Progress{Phase: release.PhaseDownloading, Version: newest.Version, Size: pkg.Size}
	if automatic {
		r.autoTried = newest.Version
		r.autoTriedAt = time.Now()
	}
	r.updateWanted = true
	r.wakeWorkerLocked()
	r.changedLocked()
	osServicesLogger.Info().Bool("automatic", automatic).Str("version", newest.Version).
		Str("path", pkg.Path).Msg("updating OS Services")
	return ""
}

// --- Worker: the downloads ------------------------------------------------------

func (r *osServicesUpdates) wakeWorkerLocked() {
	if !r.workerRunning {
		r.workerRunning = true
		go r.worker()
	}
}

func (r *osServicesUpdates) worker() {
	for {
		r.mu.Lock()
		refresh, update, maxAge := r.refreshWanted, r.updateWanted, r.refreshMaxAge
		r.refreshWanted, r.updateWanted = false, false
		// A stopped job's stop goes with it.
		r.stop = false
		if !refresh && !update {
			r.workerRunning = false
			r.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		r.cancel = cancel
		r.mu.Unlock()
		// The index downloads give way to a firmware update too; the next
		// check retries one skipped.
		if refresh && !firmwareUpdatePending() {
			r.refresh(ctx, maxAge)
			r.considerAutoUpdate()
		}
		if update {
			r.runUpdate(ctx)
		}
		cancel()
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
	}
}

func (r *osServicesUpdates) stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stop
}

// stoppedOr is the firmware update's stop when it stopped the job, else err.
func (r *osServicesUpdates) stoppedOr(err error) error {
	if r.stopped() {
		return errors.New(osUpdateStopped)
	}
	return err
}

type osUpdateFile struct {
	status int
	body   []byte // for a 200 only
	etag   string
}

// fetchOSUpdateFile GETs a small file (an index or a signature) of at most
// limit bytes.
func fetchOSUpdateFile(ctx context.Context, client *http.Client, url, ifNoneMatch string, limit int) (osUpdateFile, error) {
	ctx, cancel := context.WithTimeout(ctx, osUpdateIdleTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return osUpdateFile{}, err
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := client.Do(req)
	if err != nil {
		osServicesLogger.Debug().Err(err).Str("url", url).Msg("OS Services release server unreachable")
		return osUpdateFile{}, errors.New(osUpdateUnreachable)
	}
	defer resp.Body.Close()
	file := osUpdateFile{status: resp.StatusCode, etag: resp.Header.Get("ETag")}
	if resp.StatusCode != http.StatusOK {
		return file, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	switch {
	case err != nil:
		return file, errors.New("the OS Services release download failed")
	case len(body) == 0 || len(body) > limit:
		return file, errors.New("an OS Services release file of an unexpected size")
	}
	file.body = body
	return file, nil
}

// releaseKeys makes sure the verifier holds the JetKVM release key, from
// the keyservers or its 24-hour cache.
func (r *osServicesUpdates) releaseKeys(ctx context.Context) error {
	if _, err := r.verifier.FetchPublicKey(ctx); err != nil {
		osServicesLogger.Warn().Err(err).Msg("JetKVM release key unavailable")
		return r.stoppedOr(errors.New("the JetKVM release key is unavailable"))
	}
	return nil
}

// fetchIndex fetches one index into its cached copy. Nothing changes on
// failure.
func (r *osServicesUpdates) fetchIndex(ctx context.Context, client *http.Client, which int) error {
	url := osServicesBucketURL + "/" + osUpdateIndexNames[which]
	r.mu.Lock()
	etag := r.etags[which]
	r.mu.Unlock()
	index, err := fetchOSUpdateFile(ctx, client, url, etag, release.IndexMax)
	fresh := err == nil && index.status == http.StatusOK
	if fresh && which == osUpdateIndexStable {
		var sig osUpdateFile
		sig, err = fetchOSUpdateFile(ctx, client, url+".sig", "", release.SignatureMax)
		if err == nil && sig.status != http.StatusOK {
			err = errors.New("the OS Services release index has no signature")
		}
		if err == nil {
			err = r.releaseKeys(ctx)
		}
		if err == nil {
			if verifyErr := r.verifier.VerifySignature(ctx, sig.body, index.body); verifyErr != nil {
				osServicesLogger.Warn().Err(verifyErr).Msg("OS Services release index signature")
				err = errors.New("the OS Services release index is not signed with the JetKVM release key")
			}
		}
	}
	var newest release.Release
	if fresh && err == nil {
		newest, err = release.ReadIndex(index.body, which == osUpdateIndexStable)
	}
	// A dev index that does not exist lists no releases.
	gone := err == nil && index.status == http.StatusNotFound && which == osUpdateIndexDev
	if err == nil && !fresh && !gone && index.status != http.StatusNotModified {
		err = errors.New("the OS Services release index is unavailable")
	}
	if err != nil || index.status == http.StatusNotModified {
		return err
	}
	r.mu.Lock()
	r.releases[which] = newest
	r.etags[which] = ""
	if fresh {
		r.etags[which] = index.etag
	}
	r.changedLocked()
	r.mu.Unlock()
	return nil
}

// refresh fetches the indexes unless the copies are younger than maxAge.
func (r *osServicesUpdates) refresh(ctx context.Context, maxAge time.Duration) {
	r.mu.Lock()
	includeDev := r.includeDev
	fresh := !r.fetchedAt.IsZero() && time.Since(r.fetchedAt) < maxAge && (r.devFetched || !includeDev)
	r.mu.Unlock()
	if fresh {
		return
	}
	client := osServicesHTTPClient()
	defer client.CloseIdleConnections()
	err := r.fetchIndex(ctx, client, osUpdateIndexStable)
	if err == nil && includeDev {
		err = r.fetchIndex(ctx, client, osUpdateIndexDev)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		osServicesLogger.Warn().Err(err).Msg("OS Services release index not refreshed")
		r.retryLocked()
		return
	}
	r.fetchedAt = time.Now()
	r.devFetched = includeDev
}

// considerAutoUpdate: after a refresh, install by itself while auto-update
// is on and no browser session is open; a version that did not install
// waits an hour.
func (r *osServicesUpdates) considerAutoUpdate() {
	browser := getActiveSessions() > 0
	r.mu.Lock()
	defer r.mu.Unlock()
	newest := r.newestLocked()
	if !browser && r.automatic && newest != nil &&
		(r.autoTried != newest.Version || time.Since(r.autoTriedAt) >= osUpdateAutoRetry) {
		r.startLocked(true)
	}
}

func (r *osServicesUpdates) runUpdate(ctx context.Context) {
	r.mu.Lock()
	// startLocked refuses while this runs, so the package stays.
	pkg, signed, version, session := r.pkg, r.pkgSigned, r.progress.Version, r.updateSession
	r.updateID = 0
	r.ended = false
	// No verdict from the sender yet.
	r.status = osservices.UpdateCancelled
	r.message = ""
	r.mu.Unlock()
	err := errors.New(osUpdateFirmwareUpdating)
	if !firmwareUpdatePending() {
		err = r.relayPackage(ctx, pkg, signed, version, session)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// What the session still says about this update is no longer heard.
	r.ended = true
	if err != nil {
		osServicesLogger.Warn().Err(err).Str("version", version).Msg("OS Services update failed")
		r.finishLocked(release.PhaseFailed, err.Error())
	}
}

// relayPackage downloads, checks and relays the chosen package.
func (r *osServicesUpdates) relayPackage(ctx context.Context, pkg release.Package, signed bool, version string, session uint32) error {
	client := osServicesHTTPClient()
	defer client.CloseIdleConnections()
	url := osServicesBucketURL + "/" + pkg.Path
	var signature []byte
	if signed {
		sig, err := fetchOSUpdateFile(ctx, client, url+".sig", "", release.SignatureMax)
		if err == nil && sig.status != http.StatusOK {
			err = errors.New("the package has no signature")
		}
		if err == nil {
			err = r.releaseKeys(ctx)
		}
		if err != nil {
			return r.stoppedOr(err)
		}
		signature = sig.body
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// The size must be the package's, not that of a compressed transfer.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		osServicesLogger.Debug().Err(err).Str("url", url).Msg("OS Services package unreachable")
		return r.stoppedOr(errors.New(osUpdateUnreachable))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength != int64(pkg.Size) {
		return errors.New("the package is not on the release server")
	}

	var sig release.Signature
	if signed {
		sig = release.StreamSignature(func(data io.Reader) error {
			return r.verifier.VerifySignatureReader(ctx, signature, data)
		})
	}
	check, err := release.NewCheck(pkg.Size, pkg.SHA256, sig)
	if err != nil {
		return err
	}
	body := newIdleReader(resp.Body, osUpdateIdleTimeout, cancel)
	defer body.stop()
	return r.transfer(body, check, pkg, version, session)
}

// transfer sends update.begin, the package through the checks, and waits
// for the sender's verdict.
func (r *osServicesUpdates) transfer(body io.Reader, check *release.Check, pkg release.Package, version string, session uint32) error {
	var id uint32
	reason := osUpdateStopped
	if !r.stopped() {
		reason = "the OS Services sender changed"
		r.svc.do(func(s *osservices.Session) {
			if s.Counters.SessionsOpened == session {
				id, reason = s.UpdateBegin(pkg.Size, pkg.SHA256, version)
			}
		})
	}
	if id == 0 {
		check.Abort()
		return errors.New(reason)
	}
	r.mu.Lock()
	r.updateID = id
	r.progress.Phase = release.PhaseSending
	r.changedLocked()
	r.mu.Unlock()

	err := release.Relay(body, func(data []byte) bool { return r.send(id, data) }, check, osservices.ChunkMaxData)
	r.poll()
	if r.stopped() && !r.senderInstalling() {
		err = errors.New(osUpdateStopped)
	}
	if err != nil {
		// The sender, or the session, ended the update: its reason says
		// more.
		if errors.Is(err, release.ErrEnded) {
			if message := r.endedMessage(); message != "" {
				err = errors.New(message)
			}
		}
		r.svc.do(func(s *osservices.Session) { s.UpdateCancel(id, err.Error()) })
		r.waitSettled()
		return err
	}
	r.waitSettled()
	if r.senderInstalling() {
		return nil
	}
	return r.stoppedOr(errors.New(r.endedMessage()))
}

// send hands data to the sender as its credit allows; false once the
// update has ended.
func (r *osServicesUpdates) send(id uint32, data []byte) bool {
	for {
		var n int
		open := false
		r.svc.do(func(s *osservices.Session) {
			n = s.UpdateSend(id, data)
			u := s.Update()
			open = u.UpdateID == id && !u.Ended
		})
		data = data[n:]
		if len(data) == 0 {
			return true
		}
		if !open || r.stopped() {
			return false
		}
		if n == 0 {
			time.Sleep(osUpdateCreditPoll)
		}
	}
}

// poll takes the session's update now, as the service does each tick.
func (r *osServicesUpdates) poll() {
	var u osservices.Update
	r.svc.do(func(s *osservices.Session) { u = s.Update() })
	r.mu.Lock()
	r.followUpdateLocked(u)
	r.mu.Unlock()
}

// waitSettled waits for the update's end: the sender's status, the KVM's
// cancel, or the session's own end or idle timeout.
func (r *osServicesUpdates) waitSettled() {
	for {
		r.poll()
		r.mu.Lock()
		ended := r.ended
		r.mu.Unlock()
		if ended {
			return
		}
		time.Sleep(osUpdateSettlePoll)
	}
}

// senderInstalling: the sender has every byte and installs; a firmware
// update no longer stops it.
func (r *osServicesUpdates) senderInstalling() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ended && r.status == osservices.UpdateInstalling
}

func (r *osServicesUpdates) endedMessage() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended {
		return ""
	}
	return r.message
}

// idleReader cancels the download when one read waits longer than idle.
// Waits for the sender's credit do not count: the session's own idle
// timeout covers them.
type idleReader struct {
	r     io.Reader
	idle  time.Duration
	timer *time.Timer
}

func newIdleReader(r io.Reader, idle time.Duration, expire func()) *idleReader {
	timer := time.AfterFunc(idle, expire)
	timer.Stop()
	return &idleReader{r: r, idle: idle, timer: timer}
}

func (i *idleReader) Read(p []byte) (int, error) {
	i.timer.Reset(i.idle)
	n, err := i.r.Read(p)
	i.timer.Stop()
	return n, err
}

func (i *idleReader) stop() { i.timer.Stop() }
