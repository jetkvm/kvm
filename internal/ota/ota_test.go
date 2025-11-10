package ota

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func pseudoGetLocalVersion() (systemVersion *semver.Version, appVersion *semver.Version, err error) {
	systemVersion = semver.MustParse("0.2.5")
	appVersion = semver.MustParse("0.4.7")
	return systemVersion, appVersion, nil
}

func newOtaState() *State {
	logger := zerolog.New(os.Stdout).Level(zerolog.TraceLevel)
	otaState := NewState(Options{
		SkipConfirmSystem:  true,
		Logger:             &logger,
		ReleaseAPIEndpoint: "https://api.jetkvm.com/releases",
		GetHTTPClient: func() *http.Client {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			client := &http.Client{
				Transport: transport,
			}
			return client
		},
		GetLocalVersion:  pseudoGetLocalVersion,
		HwReboot:         func(force bool, postRebootAction *PostRebootAction, delay time.Duration) error { return nil },
		ResetConfig:      func() error { return nil },
		OnStateUpdate:    func(state *RPCState) {},
		OnProgressUpdate: func(progress float32) {},
	})
	return otaState
}

func TestCheckUpdateComponents(t *testing.T) {
	otaState := newOtaState()
	updateParams := UpdateParams{
		DeviceID:            "test",
		IncludePreRelease:   false,
		SystemTargetVersion: "0.2.2",
		Components:          []string{"system"},
	}
	info, err := otaState.GetUpdateStatus(context.Background(), updateParams)
	t.Logf("update status: %+v", info)
	if err != nil {
		t.Fatalf("failed to check update: %v", err)
	}
	assert.True(t, info.SystemUpdateAvailable)
	assert.False(t, info.AppUpdateAvailable)
}
