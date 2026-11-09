package kvm

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/osservices"
	"github.com/jetkvm/kvm/internal/osservices/ffs"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
)

// IP-KVM OS Services: a service on the target computer talks to the KVM
// over the KVM's USB cable (jetkvm/os-services PROTOCOL.md). The protocol
// runs in internal/osservices; this file connects it to the USB gadget, the
// settings, the browser and the video pipeline.

var osServicesLogger = logging.GetSubsystemLogger("os_services")

const (
	osServicesUSBKey      = "os_services"
	osServicesFFSInstance = "os_services"
	osServicesFFSMount    = "/dev/ffs-os-services"
	osServicesKeyPath     = "/userdata/jetkvm/os_services.key"
	osServicesTick        = 50 * time.Millisecond
	// The RPC waits this long for a clipboard answer: the session's own
	// timeout, a write and some slack.
	osServicesClipboardWait = osservices.ClipboardTimeoutMS*time.Millisecond + 2500*time.Millisecond
)

var osServices *osServicesService

type clipboardResult struct {
	status    osservices.ClipboardStatus
	answered  bool
	text      string
	truncated bool
}

type osServicesService struct {
	link    *ffs.Link
	session *osservices.Session // guarded by link.Mu

	key         ed25519.PrivateKey
	fingerprint string
	start       time.Time

	fnMu sync.Mutex
	fn   *ffs.Function

	settingsMu       sync.Mutex
	settingsSequence uint32

	stateSignal chan struct{}
	// The source last logged; guarded by link.Mu.
	lastSource osservices.VideoSource

	// Guarded by link.Mu.
	clipboardWaits map[uint32]chan clipboardResult
	terminals      map[uint32]*guestTerminal
	drops          map[uint32]*guestDrop

	video guestVideo
}

// initOSServices sets up OS Services before the USB gadget, which links the
// OS Services function once its descriptors are written.
func initOSServices() {
	if failsafeModeActive {
		return
	}
	key, err := loadOSServicesKey(osServicesKeyPath)
	if err != nil {
		osServicesLogger.Error().Err(err).Msg("OS Services disabled: no KVM key")
		return
	}
	digest := sha256.Sum256(key.Public().(ed25519.PublicKey))
	s := &osServicesService{
		key:            key,
		fingerprint:    osservices.FormatFingerprint(digest[:]),
		start:          time.Now(),
		stateSignal:    make(chan struct{}, 1),
		clipboardWaits: map[uint32]chan clipboardResult{},
		terminals:      map[uint32]*guestTerminal{},
		drops:          map[uint32]*guestDrop{},
	}
	s.video.init(s)
	udc := ""
	if udcs, _ := filepath.Glob("/sys/class/udc/*"); len(udcs) > 0 {
		udc = filepath.Base(udcs[0])
	}
	s.link = ffs.NewLink(osServicesLogger, udc)
	s.session = osservices.New(s, s.config().sessionSettings(osservices.CodecH264))
	s.session.SetFingerprint(s.fingerprint)
	s.link.SetSession(s.session)
	osServices = s

	usbgadget.RegisterExternalFunction(usbgadget.ExternalFunction{
		Key:             osServicesUSBKey,
		Instance:        osServicesFFSInstance,
		Order:           5000,
		Prepare:         s.prepareFunction,
		MSOSDescriptors: true,
	})
	go s.ticker()
	go s.stateNotifier()
	osServicesLogger.Info().Str("fingerprint", s.fingerprint).Msg("OS Services ready")
}

// loadOSServicesKey reads the KVM's Ed25519 seed, or makes one. The key stays
// until a factory reset.
func loadOSServicesKey(path string) (ed25519.PrivateKey, error) {
	seed, err := os.ReadFile(path)
	if err == nil && len(seed) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	seed = make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, seed, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// prepareFunction mounts the FunctionFS instance and writes its descriptors,
// once; usbgadget calls it before each configuration.
func (s *osServicesService) prepareFunction() error {
	s.fnMu.Lock()
	defer s.fnMu.Unlock()
	if s.fn != nil {
		return nil
	}
	if err := ffs.Mount(osServicesFFSMount, osServicesFFSInstance); err != nil {
		return err
	}
	fn, err := ffs.Open(osServicesFFSMount)
	if err != nil {
		return err
	}
	s.fn = fn
	s.link.Start(fn)
	osServicesLogger.Info().Msg("OS Services USB function ready")
	return nil
}

// osServicesNoteKVMRestart: the KVM restarts its own USB device; a session
// that ends for it does not count against the video retry budget.
func osServicesNoteKVMRestart() {
	if osServices != nil {
		osServices.link.NoteKVMRestart()
	}
}

func (s *osServicesService) config() OSServicesConfig {
	configLock.Lock()
	defer configLock.Unlock()
	if config == nil || config.OSServices == nil {
		return *defaultOSServicesConfig()
	}
	return *config.OSServices
}

// do runs fn with the session lock held.
func (s *osServicesService) do(fn func(*osservices.Session)) {
	s.link.Mu.Lock()
	defer s.link.Mu.Unlock()
	fn(s.session)
}

func (s *osServicesService) ticker() {
	t := time.NewTicker(osServicesTick)
	defer t.Stop()
	for range t.C {
		s.link.Mu.Lock()
		s.session.Tick()
		s.video.tick(s.session)
		s.flushTerminalCredits()
		s.link.Mu.Unlock()
		s.link.Wake()
	}
}

// applySettings hands the settings and the browser's codec to the session.
func (s *osServicesService) applySettings() {
	s.applySettingsFor(currentSession)
}

func (s *osServicesService) applySettingsFor(browser *Session) {
	settings := s.config().sessionSettings(browserCodec(browser))
	s.do(func(session *osservices.Session) { session.ApplySettings(settings) })
}

// browserCodec is the codec a browser session negotiated.
func browserCodec(session *Session) osservices.Codec {
	if session != nil && session.codecMimeType == webrtc.MimeTypeH265 {
		return osservices.CodecH265
	}
	return osservices.CodecH264
}

// --- Platform -----------------------------------------------------------------

func (s *osServicesService) NowMS() int64 { return time.Since(s.start).Milliseconds() }
func (s *osServicesService) NowUS() int64 { return time.Since(s.start).Microseconds() }

func (s *osServicesService) FillRandom(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
}

func (s *osServicesService) PublicKey() ([]byte, bool) {
	return s.key.Public().(ed25519.PublicKey), true
}

func (s *osServicesService) Sign(message []byte) ([]byte, bool) {
	return ed25519.Sign(s.key, message), true
}

func (s *osServicesService) Write(envelope, head, body []byte) bool {
	return s.link.Write(envelope, head, body)
}

func (s *osServicesService) SessionEnded(reason string) {
	osServicesLogger.Info().Str("reason", reason).Msg("OS Services session ended")
	s.link.SessionEnded()
}

func (s *osServicesService) ResetTransport() { s.link.ResetTransport() }

func (s *osServicesService) StateChanged() {
	if s.session == nil {
		return
	}
	if source := s.session.Source(); source != s.lastSource {
		s.lastSource = source
		osServicesLogger.Info().Str("session", s.session.String()).Bool("owned", s.video.owned.Load()).
			Bool("presented", s.video.presented.Load()).Msg("video source changed")
	}
	select {
	case s.stateSignal <- struct{}{}:
	default:
	}
}

func (s *osServicesService) ClipboardDone(id uint32, status osservices.ClipboardStatus, answered bool, text []byte, truncated bool) {
	wait, ok := s.clipboardWaits[id]
	if !ok {
		return
	}
	delete(s.clipboardWaits, id)
	wait <- clipboardResult{status: status, answered: answered, text: string(text), truncated: truncated}
}

// stateNotifier sends the browser the changed state, at most every 100 ms.
func (s *osServicesService) stateNotifier() {
	var last string
	for range s.stateSignal {
		state := currentVideoState()
		if text := fmt.Sprintf("%+v", state); text != last {
			last = text
			writeJSONRPCEvent("videoInputState", state, currentSession)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// --- State for the browser ------------------------------------------------------

type osServicesSenderJSON struct {
	Version  string   `json:"version"`
	Displays []string `json:"displays"`
}

type osServicesStatusJSON struct {
	Sender          *osServicesSenderJSON `json:"sender,omitempty"`
	Connected       bool                  `json:"connected"`
	Video           bool                  `json:"video"`
	VideoRegistered bool                  `json:"videoRegistered"`
	VideoError      string                `json:"videoError,omitempty"`
	Clipboard       bool                  `json:"clipboard"`
	ApprovalPending bool                  `json:"approvalPending"`
	Fingerprint     string                `json:"fingerprint"`
}

type guestClipboardJSON struct {
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
	OS    string `json:"os"`
}

type guestFileJSON struct {
	Receive bool `json:"receive"`
}

// rpcVideoState is videoInputState and the getVideoState answer: the video
// state, and on a KVM with OS Services the source and the sender.
type rpcVideoState struct {
	Ready          bool    `json:"ready"`
	Streaming      int     `json:"streaming"`
	Error          string  `json:"error,omitempty"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`

	Source         string                `json:"source,omitempty"`
	OSServices     *osServicesStatusJSON `json:"osServices,omitempty"`
	GuestClipboard *guestClipboardJSON   `json:"guestClipboard,omitempty"`
	GuestTerminal  bool                  `json:"guestTerminal,omitempty"`
	GuestFile      *guestFileJSON        `json:"guestFile,omitempty"`
	GuestUpdate    any                   `json:"guestUpdate,omitempty"`
}

func currentVideoState() rpcVideoState {
	v := lastVideoState
	state := rpcVideoState{
		Ready:          v.Ready,
		Streaming:      int(v.Streaming),
		Error:          v.Error,
		Width:          v.Width,
		Height:         v.Height,
		FramePerSecond: v.FramePerSecond,
	}
	if osServices == nil {
		return state
	}
	var st osservices.State
	osServices.do(func(session *osservices.Session) { st = session.State() })
	state.Source = st.Source.String()
	switch st.Source {
	case osservices.SourceGuest:
		state.Ready = true
		state.Error = ""
		if st.StreamStarted {
			state.Width = int(st.StreamWidth)
			state.Height = int(st.StreamHeight)
			state.FramePerSecond = float64(st.StreamFPS)
		}
	case osservices.SourceSwitchingToGuest, osservices.SourceSwitchingToPhysical:
		// The hardware capture's state during a switch is no signal, which
		// the browser does not show then.
		state.Ready = false
		state.Error = ""
	}
	status := &osServicesStatusJSON{
		Connected:       st.Connected,
		Video:           st.Source == osservices.SourceGuest,
		VideoRegistered: st.VideoRegistered,
		VideoError:      st.VideoError,
		Clipboard:       st.Clipboard,
		ApprovalPending: st.ApprovalPending,
		Fingerprint:     st.Fingerprint,
	}
	if st.SenderInfo {
		displays := st.Displays
		if displays == nil {
			displays = []string{}
		}
		status.Sender = &osServicesSenderJSON{Version: st.SenderVersion, Displays: displays}
	}
	state.OSServices = status
	if st.Clipboard {
		state.GuestClipboard = &guestClipboardJSON{
			Read:  st.ClipboardFlags&osservices.ClipboardFlagRead != 0,
			Write: st.ClipboardFlags&osservices.ClipboardFlagWrite != 0,
			OS:    st.ClipboardOS.String(),
		}
	}
	state.GuestTerminal = st.Terminal
	if st.FileReceive {
		state.GuestFile = &guestFileJSON{Receive: true}
	}
	if update := osServicesUpdateState(st); update != nil {
		state.GuestUpdate = update
	}
	return state
}

// --- RPC ------------------------------------------------------------------------

func requireOSServices() (*osServicesService, error) {
	if osServices == nil {
		return nil, errors.New("OS Services unavailable")
	}
	return osServices, nil
}

func rpcGetOsServicesSettings() (OSServicesConfig, error) {
	s, err := requireOSServices()
	if err != nil {
		return OSServicesConfig{}, err
	}
	return s.config(), nil
}

type osServicesSettingsEvent struct {
	Sequence uint32           `json:"sequence"`
	Settings OSServicesConfig `json:"settings"`
}

func rpcSetOsServicesSettings(settings map[string]any) (OSServicesConfig, error) {
	s, err := requireOSServices()
	if err != nil {
		return OSServicesConfig{}, err
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	current := s.config()
	merged, err := mergeOSServicesConfig(current, settings)
	if err != nil {
		return current, err
	}
	if merged == current {
		return current, nil
	}
	configLock.Lock()
	stored := merged
	config.OSServices = &stored
	configLock.Unlock()
	if err := SaveConfig(); err != nil {
		return merged, fmt.Errorf("failed to save config: %w", err)
	}
	s.applySettings()
	s.settingsSequence++
	event := osServicesSettingsEvent{Sequence: s.settingsSequence, Settings: merged}
	go writeJSONRPCEvent("osServicesSettings", event, currentSession)
	s.StateChanged()
	return merged, nil
}

const (
	clipboardUnavailable = "no OS Services clipboard is connected"
	clipboardDisabled    = "clipboard sharing is turned off in the OS Services settings"
	clipboardNoAnswer    = "the target computer's clipboard did not answer in time"
)

func (s *osServicesService) clipboardCall(text []byte, set bool) (clipboardResult, error) {
	wait := make(chan clipboardResult, 1)
	var startErr error
	s.do(func(session *osservices.Session) {
		id, status := session.ClipboardRequest(text, set)
		switch {
		case id != 0:
			s.clipboardWaits[id] = wait
		case !session.Settings().ClipboardEnabled:
			startErr = errors.New(clipboardDisabled)
		case status == osservices.ClipboardUnsupported:
			startErr = errors.New(clipboardUnavailable)
		default:
			startErr = errors.New("the clipboard request could not be sent")
		}
	})
	if startErr != nil {
		return clipboardResult{}, startErr
	}
	select {
	case result := <-wait:
		if !result.answered {
			open := false
			s.do(func(session *osservices.Session) { open = session.Open() })
			if open {
				return result, errors.New(clipboardNoAnswer)
			}
			return result, errors.New(clipboardUnavailable)
		}
		return result, nil
	case <-time.After(osServicesClipboardWait):
		return clipboardResult{}, errors.New(clipboardNoAnswer)
	}
}

type guestClipboardGetResult struct {
	Status    string `json:"status"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func rpcGuestClipboardGet() (guestClipboardGetResult, error) {
	s, err := requireOSServices()
	if err != nil {
		return guestClipboardGetResult{}, errors.New(clipboardUnavailable)
	}
	result, err := s.clipboardCall(nil, false)
	if err != nil {
		return guestClipboardGetResult{}, err
	}
	answer := guestClipboardGetResult{Status: result.status.String(), Truncated: result.truncated}
	if result.status == osservices.ClipboardOK {
		answer.Text = result.text
	}
	return answer, nil
}

type guestClipboardSetResult struct {
	Status string `json:"status"`
}

func rpcGuestClipboardSet(text string) (guestClipboardSetResult, error) {
	s, err := requireOSServices()
	if err != nil {
		return guestClipboardSetResult{}, errors.New(clipboardUnavailable)
	}
	if len(text) > osservices.ClipboardMaxText {
		return guestClipboardSetResult{}, fmt.Errorf("clipboard text exceeds %d bytes", osservices.ClipboardMaxText)
	}
	if !osservices.UTF8Valid([]byte(text)) {
		return guestClipboardSetResult{}, errors.New("clipboard text is not valid UTF-8")
	}
	result, err := s.clipboardCall([]byte(text), true)
	if err != nil {
		return guestClipboardSetResult{}, err
	}
	return guestClipboardSetResult{Status: result.status.String()}, nil
}

// --- Helpers for the rest of the firmware ----------------------------------------

// osServicesOwnsVideo reports whether service video owns the display: the
// hardware capture stays off and its frames go nowhere.
func osServicesOwnsVideo() bool {
	return osServices != nil && osServices.video.owned.Load()
}

// osServicesDisplayEDID is the EDID of the KVM display while service video
// owns it, else "".
func osServicesDisplayEDID() string {
	if osServices == nil {
		return ""
	}
	if edid, ok := osServices.video.edid.Load().(string); ok {
		return edid
	}
	return ""
}

// osServicesPrefersH264: a sender shows video without H.265, so a new
// browser session takes H.264.
func osServicesPrefersH264() bool {
	if osServices == nil {
		return false
	}
	prefer := false
	osServices.do(func(session *osservices.Session) {
		st := session.State()
		prefer = st.VideoRegistered && st.VideoCodecFlags&osservices.VideoCodecFlagH265 == 0 &&
			session.Settings().VideoEnabled
	})
	return prefer
}

// osServicesBrowserChanged: a browser session connected; it needs the codec
// it negotiated and a keyframe. currentSession may not point at it yet.
func osServicesBrowserChanged(session *Session) {
	if osServices == nil {
		return
	}
	osServices.applySettingsFor(session)
	osServices.video.wantKeyframe()
}

// osServicesKeyframeRequest: the browser lost video (PLI or FIR).
func osServicesKeyframeRequest() {
	if osServicesOwnsVideo() {
		osServices.video.wantKeyframe()
	}
}
