package ota

import (
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

var (
	availableComponents = []string{"app", "system"}
	defaultComponents   = map[string]string{
		"app":    "",
		"system": "",
	}
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
	WillDisableAutoUpdate bool            `json:"willDisableAutoUpdate"`

	// only available for debugging and won't be exported
	SystemUpdateAvailableReason string `json:"-"`
	AppUpdateAvailableReason    string `json:"-"`

	// for backwards compatibility
	Error string `json:"error,omitempty"`
}

// PostRebootAction represents the action to be taken after a reboot
// It is used to redirect the user to a specific page after a reboot
type PostRebootAction struct {
	HealthCheck string `json:"healthCheck"` // The health check URL to call after the reboot
	RedirectTo  string `json:"redirectTo"`  // The URL to redirect to after the reboot
}

// componentUpdateStatus represents the status of a component update
type componentUpdateStatus struct {
	pending              bool
	available            bool
	availableReason      string // why the component is available or not available
	customVersionUpdate  bool
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

func (c *componentUpdateStatus) getZerologLogger(l *zerolog.Logger) *zerolog.Logger {
	logger := l.With().
		Bool("pending", c.pending).
		Bool("available", c.available).
		Str("availableReason", c.availableReason).
		Str("version", c.version).
		Str("localVersion", c.localVersion).
		Str("url", c.url).
		Str("hash", c.hash).
		Float32("downloadProgress", c.downloadProgress).
		Time("downloadFinishedAt", c.downloadFinishedAt).
		Float32("verificationProgress", c.verificationProgress).
		Time("verifiedAt", c.verifiedAt).
		Float32("updateProgress", c.updateProgress).
		Time("updatedAt", c.updatedAt).
		Strs("dependsOn", c.dependsOn).
		Logger()
	return &logger
}

// HwRebootFunc is a function that reboots the hardware
type HwRebootFunc func(force bool, postRebootAction *PostRebootAction, delay time.Duration) error

// ResetConfigFunc is a function that resets the config
type ResetConfigFunc func() error

// SetAutoUpdateFunc is a function that sets the auto-update state
type SetAutoUpdateFunc func(enabled bool) (bool, error)

// GetHTTPClientFunc is a function that returns the HTTP client
type GetHTTPClientFunc func() HttpClient

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
	onStateUpdate           OnStateUpdateFunc
	resetConfig             ResetConfigFunc
	setAutoUpdate           SetAutoUpdateFunc
}

func toUpdateStatus(appUpdate *componentUpdateStatus, systemUpdate *componentUpdateStatus, error string) *UpdateStatus {
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
		SystemUpdateAvailable:       systemUpdate.available,
		SystemUpdateAvailableReason: systemUpdate.availableReason,
		AppUpdateAvailable:          appUpdate.available,
		AppUpdateAvailableReason:    appUpdate.availableReason,
		WillDisableAutoUpdate:       appUpdate.customVersionUpdate || systemUpdate.customVersionUpdate,
		Error:                       error,
	}
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

	return toUpdateStatus(&appUpdate, &systemUpdate, s.error)
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
	ResetConfig        ResetConfigFunc
	SkipConfirmSystem  bool
	SetAutoUpdate      SetAutoUpdateFunc
}

// NewState creates a new OTA state
func NewState(opts Options) *State {
	components := make(map[string]componentUpdateStatus)
	for _, component := range availableComponents {
		components[component] = componentUpdateStatus{}
	}

	s := &State{
		l:                       opts.Logger,
		client:                  opts.GetHTTPClient,
		reboot:                  opts.HwReboot,
		onStateUpdate:           opts.OnStateUpdate,
		getLocalVersion:         opts.GetLocalVersion,
		componentUpdateStatuses: components,
		releaseAPIEndpoint:      opts.ReleaseAPIEndpoint,
		resetConfig:             opts.ResetConfig,
		setAutoUpdate:           opts.SetAutoUpdate,
	}
	if !opts.SkipConfirmSystem {
		go s.confirmCurrentSystem()
	}
	return s
}
