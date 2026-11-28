package release

// Phase is where an update is.
type Phase int

const (
	PhaseNone Phase = iota
	PhaseDownloading
	PhaseSending
	PhaseInstalling
	PhaseDone
	PhaseFailed
)

var phaseNames = [...]string{"", "downloading", "sending", "installing", "done", "failed"}

func (p Phase) String() string { return phaseNames[p] }

// Progress is the update in progress, or the last one.
type Progress struct {
	Phase   Phase
	Version string
	Sent    uint64
	Size    uint64
	Error   string
}

// Running: the update is downloading, sending or installing.
func (p Progress) Running() bool {
	return p.Phase == PhaseDownloading || p.Phase == PhaseSending || p.Phase == PhaseInstalling
}

// Finished: the update is done or failed.
func (p Progress) Finished() bool {
	return p.Phase == PhaseDone || p.Phase == PhaseFailed
}

// GuestUpdate is the browser's guestUpdate object (GuestUpdateState in the
// UI's stores.ts).
type GuestUpdate struct {
	Version     string `json:"version"`
	Available   bool   `json:"available"`
	Installable bool   `json:"installable"`
	Automatic   bool   `json:"automatic"`
	*GuestUpdateProgress
}

// GuestUpdateProgress is present while the object shows an update.
type GuestUpdateProgress struct {
	Phase string `json:"phase"`
	Sent  uint64 `json:"sent"`
	Size  uint64 `json:"size"`
	Error string `json:"error,omitempty"`
}

// Render is the browser's guestUpdate: the newest release and whether the
// sender can take it, or the update in progress (or the one that just
// ended, while no newer release replaced it). nil while neither exists.
func Render(newest *Release, sender Sender, progress Progress, automatic bool) *GuestUpdate {
	// A finished update gives way to a newer release; a running one does
	// not.
	showProgress := progress.Phase != PhaseNone &&
		(!progress.Finished() || newest == nil || newest.Version == progress.Version)
	version := ""
	switch {
	case showProgress:
		version = progress.Version
	case newest != nil:
		version = newest.Version
	}
	if version == "" {
		return nil
	}
	state := &GuestUpdate{
		Version:     version,
		Available:   newest != nil && Newer(newest.Version, sender.Version),
		Installable: newest.Package(sender.Package) != nil,
		Automatic:   automatic,
	}
	if showProgress {
		state.GuestUpdateProgress = &GuestUpdateProgress{
			Phase: progress.Phase.String(),
			Sent:  progress.Sent,
			Size:  progress.Size,
			Error: progress.Error,
		}
	}
	return state
}
