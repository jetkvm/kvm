package ota

import (
	"net/http"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

// UpdateMetadata represents the metadata of an update
type UpdateMetadata struct {
	AppVersion    string `json:"appVersion"`
	AppURL        string `json:"appUrl"`
	AppHash       string `json:"appHash"`
	SystemVersion string `json:"systemVersion"`
	SystemURL     string `json:"systemUrl"`
	SystemHash    string `json:"systemHash"`
}

// LocalMetadata represents the local metadata of the system
type LocalMetadata struct {
	AppVersion    string `json:"appVersion"`
	SystemVersion string `json:"systemVersion"`
}

// UpdateStatus represents the current update status
type UpdateStatus struct {
	Local                 *LocalMetadata  `json:"local"`
	Remote                *UpdateMetadata `json:"remote"`
	SystemUpdateAvailable bool            `json:"systemUpdateAvailable"`
	AppUpdateAvailable    bool            `json:"appUpdateAvailable"`

	// for backwards compatibility
	Error string `json:"error,omitempty"`
}

// PostRebootAction represents the action to be taken after a reboot
// It is used to redirect the user to a specific page after a reboot
type PostRebootAction struct {
	HealthCheck string `json:"healthCheck"` // The health check URL to call after the reboot
	RedirectUrl string `json:"redirectUrl"` // The URL to redirect to after the reboot
}

// componentUpdateStatus represents the status of a component update
type componentUpdateStatus struct {
	pending              bool
	available            bool
	version              string
	localVersion         string
	url                  string
	hash                 string
	downloadProgress     float32
	downloadFinishedAt   time.Time
	verificationProgress float32
	verifiedAt           time.Time
	updateProgress       float32
	updatedAt            time.Time
	dependsOn            []string
}

// RPCState represents the current OTA state for the RPC API
type RPCState struct {
	Updating                   bool      `json:"updating"`
	Error                      string    `json:"error,omitempty"`
	MetadataFetchedAt          time.Time `json:"metadataFetchedAt,omitempty"`
	AppUpdatePending           bool      `json:"appUpdatePending"`
	SystemUpdatePending        bool      `json:"systemUpdatePending"`
	AppDownloadProgress        float32   `json:"appDownloadProgress,omitempty"` //TODO: implement for progress bar
	AppDownloadFinishedAt      time.Time `json:"appDownloadFinishedAt,omitempty"`
	SystemDownloadProgress     float32   `json:"systemDownloadProgress,omitempty"` //TODO: implement for progress bar
	SystemDownloadFinishedAt   time.Time `json:"systemDownloadFinishedAt,omitempty"`
	AppVerificationProgress    float32   `json:"appVerificationProgress,omitempty"`
	AppVerifiedAt              time.Time `json:"appVerifiedAt,omitempty"`
	SystemVerificationProgress float32   `json:"systemVerificationProgress,omitempty"`
	SystemVerifiedAt           time.Time `json:"systemVerifiedAt,omitempty"`
	AppUpdateProgress          float32   `json:"appUpdateProgress,omitempty"` //TODO: implement for progress bar
	AppUpdatedAt               time.Time `json:"appUpdatedAt,omitempty"`
	SystemUpdateProgress       float32   `json:"systemUpdateProgress,omitempty"` //TODO: port rk_ota, then implement
	SystemUpdatedAt            time.Time `json:"systemUpdatedAt,omitempty"`
}

// HwRebootFunc is a function that reboots the hardware
type HwRebootFunc func(force bool, postRebootAction *PostRebootAction, delay time.Duration) error

// GetHTTPClientFunc is a function that returns the HTTP client
type GetHTTPClientFunc func() *http.Client

// OnStateUpdateFunc is a function that updates the state of the OTA
type OnStateUpdateFunc func(state *RPCState)

// OnProgressUpdateFunc is a function that updates the progress of the OTA
type OnProgressUpdateFunc func(progress float32)

// GetLocalVersionFunc is a function that returns the local version of the system and app
type GetLocalVersionFunc func() (systemVersion *semver.Version, appVersion *semver.Version, err error)

// State represents the current OTA state for the UI
type State struct {
	releaseAPIEndpoint      string
	l                       *zerolog.Logger
	mu                      sync.Mutex
	updating                bool
	error                   string
	metadataFetchedAt       time.Time
	rebootNeeded            bool
	componentUpdateStatuses map[string]componentUpdateStatus
	client                  GetHTTPClientFunc
	reboot                  HwRebootFunc
	getLocalVersion         GetLocalVersionFunc
}

// ToUpdateStatus converts the State to the UpdateStatus
func (s *State) ToUpdateStatus() *UpdateStatus {
	appUpdate, ok := s.componentUpdateStatuses["app"]
	if !ok {
		return nil
	}

	systemUpdate, ok := s.componentUpdateStatuses["system"]
	if !ok {
		return nil
	}

	return &UpdateStatus{
		Local: &LocalMetadata{
			AppVersion:    appUpdate.localVersion,
			SystemVersion: systemUpdate.localVersion,
		},
		Remote: &UpdateMetadata{
			AppVersion:    appUpdate.version,
			AppURL:        appUpdate.url,
			AppHash:       appUpdate.hash,
			SystemVersion: systemUpdate.version,
			SystemURL:     systemUpdate.url,
			SystemHash:    systemUpdate.hash,
		},
		SystemUpdateAvailable: systemUpdate.available,
		AppUpdateAvailable:    appUpdate.available,
		Error:                 s.error,
	}
}

// IsUpdatePending returns true if an update is pending
func (s *State) IsUpdatePending() bool {
	return s.updating
}

// Options represents the options for the OTA state
type Options struct {
	Logger             *zerolog.Logger
	GetHTTPClient      GetHTTPClientFunc
	GetLocalVersion    GetLocalVersionFunc
	OnStateUpdate      OnStateUpdateFunc
	OnProgressUpdate   OnProgressUpdateFunc
	HwReboot           HwRebootFunc
	ReleaseAPIEndpoint string
}

// NewState creates a new OTA state
func NewState(opts Options) *State {
	s := &State{
		l:                       opts.Logger,
		client:                  opts.GetHTTPClient,
		reboot:                  opts.HwReboot,
		getLocalVersion:         opts.GetLocalVersion,
		componentUpdateStatuses: make(map[string]componentUpdateStatus),
		releaseAPIEndpoint:      opts.ReleaseAPIEndpoint,
	}
	go s.confirmCurrentSystem()
	return s
}

// ToRPCState converts the State to the RPCState
func (s *State) ToRPCState() *RPCState {
	r := &RPCState{
		Updating:          s.updating,
		Error:             s.error,
		MetadataFetchedAt: s.metadataFetchedAt,
	}

	app, ok := s.componentUpdateStatuses["app"]
	if ok {
		r.AppUpdatePending = app.pending
		r.AppDownloadProgress = app.downloadProgress
		r.AppDownloadFinishedAt = app.downloadFinishedAt
		r.AppVerificationProgress = app.verificationProgress
		r.AppVerifiedAt = app.verifiedAt
		r.AppUpdateProgress = app.updateProgress
		r.AppUpdatedAt = app.updatedAt
	}

	system, ok := s.componentUpdateStatuses["system"]
	if ok {
		r.SystemUpdatePending = system.pending
		r.SystemDownloadProgress = system.downloadProgress
		r.SystemDownloadFinishedAt = system.downloadFinishedAt
		r.SystemVerificationProgress = system.verificationProgress
		r.SystemVerifiedAt = system.verifiedAt
		r.SystemUpdateProgress = system.updateProgress
		r.SystemUpdatedAt = system.updatedAt
	}

	return r
}

func (s *State) onProgressUpdate() {
}
