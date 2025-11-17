package ota

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/gwatts/rootcerts"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

const pseudoDeviceID = "golang-test"

type otaTestStateParams struct {
	LocalSystemVersion string
	LocalAppVersion    string
	WithoutCerts       bool
}

func newOtaState(p otaTestStateParams) *State {
	pseudoGetLocalVersion := func() (systemVersion *semver.Version, appVersion *semver.Version, err error) {
		appVersion = semver.MustParse(p.LocalAppVersion)
		systemVersion = semver.MustParse(p.LocalSystemVersion)
		return systemVersion, appVersion, nil
	}

	traceLevel := zerolog.InfoLevel

	if os.Getenv("TEST_LOG_TRACE") == "true" {
		traceLevel = zerolog.TraceLevel
	}
	logger := zerolog.New(os.Stdout).Level(traceLevel)
	otaState := NewState(Options{
		SkipConfirmSystem:  true,
		Logger:             &logger,
		ReleaseAPIEndpoint: "https://api.jetkvm.com/releases",
		GetHTTPClient: func() *http.Client {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			if !p.WithoutCerts {
				transport.TLSClientConfig = &tls.Config{RootCAs: rootcerts.ServerCertPool()}
			} else {
				transport.TLSClientConfig = &tls.Config{RootCAs: x509.NewCertPool()}
			}
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

func TestCheckUpdateComponentsWithoutCerts(t *testing.T) {
	otaState := newOtaState(otaTestStateParams{
		LocalSystemVersion: "0.2.5",
		LocalAppVersion:    "0.4.7",
		WithoutCerts:       true,
	})
	_, err := otaState.GetUpdateStatus(context.Background(), UpdateParams{})
	assert.ErrorContains(t, err, "certificate signed by unknown authority")
}

type updateStatusAsserts struct {
	system bool
	app    bool
	skip   bool
}

func testGetUpdateStatus(t *testing.T, p otaTestStateParams, u UpdateParams, asserts updateStatusAsserts) *UpdateStatus {
	otaState := newOtaState(p)
	info, err := otaState.GetUpdateStatus(context.Background(), u)
	t.Logf("update status: %+v", info)
	if err != nil {
		t.Fatalf("failed to check update: %v", err)
	}
	if asserts.skip {
		return info
	}

	if asserts.system {
		assert.True(t, info.SystemUpdateAvailable, fmt.Sprintf("system update should available, but reason: %s", info.SystemUpdateAvailableReason))
	} else {
		assert.False(t, info.SystemUpdateAvailable, fmt.Sprintf("system update should not be available, but reason: %s", info.SystemUpdateAvailableReason))
	}
	if asserts.app {
		assert.True(t, info.AppUpdateAvailable, fmt.Sprintf("app update should available, but reason: %s", info.AppUpdateAvailableReason))
	} else {
		assert.False(t, info.AppUpdateAvailable, fmt.Sprintf("app update should not be available, but reason: %s", info.AppUpdateAvailableReason))
	}
	return info
}

func TestCheckUpdateComponentsSystemOnlyUpgrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.2", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		IncludePreRelease: false,
		Components:        map[string]string{"system": ""},
	}, updateStatusAsserts{
		system: true,
		app:    false,
	})
}

func TestCheckUpdateComponentsSystemOnlyDowngrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.5", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		Components:        map[string]string{"system": "0.2.2"},
		IncludePreRelease: false,
	}, updateStatusAsserts{
		system: true,
		app:    false,
	})
}

func TestCheckUpdateComponentsAppOnlyUpgrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.2", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		IncludePreRelease: false,
		Components:        map[string]string{"app": ""},
	}, updateStatusAsserts{
		system: false,
		app:    true,
	})
}

func TestCheckUpdateComponentsAppOnlyDowngrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.2", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		Components:        map[string]string{"app": "0.4.6"},
		IncludePreRelease: false,
	}, updateStatusAsserts{
		system: false,
		app:    true,
	})
}

func TestCheckUpdateComponentsSystemBothUpgrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.2", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		IncludePreRelease: false,
		Components:        map[string]string{"system": "", "app": ""},
	}, updateStatusAsserts{
		system: true,
		app:    true,
	})
}

func TestCheckUpdateComponentsSystemBothDowngrade(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.5", // remote >= 0.2.5
		LocalAppVersion:    "0.4.5", // remote >= 0.4.7
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		Components:        map[string]string{"system": "0.2.2", "app": "0.4.6"},
		IncludePreRelease: false,
	}, updateStatusAsserts{
		system: true,
		app:    true,
	})
}

func TestCheckUpdateComponentsNoComponents(t *testing.T) {
	_ = testGetUpdateStatus(t, otaTestStateParams{
		LocalSystemVersion: "0.2.2",
		LocalAppVersion:    "0.4.2",
		WithoutCerts:       false,
	}, UpdateParams{
		DeviceID:          pseudoDeviceID,
		IncludePreRelease: false,
	}, updateStatusAsserts{
		system: true,
		app:    true,
	})
}
