package ota

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/gwatts/rootcerts"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/ota
var testDataFS embed.FS

const pseudoDeviceID = "golang-test"
const releaseAPIEndpoint = "https://api.jetkvm.com/releases"

type testData struct {
	Name           string `json:"name"`
	WithoutCerts   bool   `json:"withoutCerts"`
	RemoteMetadata []struct {
		Code   int               `json:"code"`
		Params map[string]string `json:"params"`
		Data   UpdateMetadata    `json:"data"`
	} `json:"remoteMetadata"`
	LocalMetadata struct {
		SystemVersion string `json:"systemVersion"`
		AppVersion    string `json:"appVersion"`
	} `json:"localMetadata"`
	UpdateParams UpdateParams `json:"updateParams"`
	Expected     struct {
		System bool   `json:"system"`
		App    bool   `json:"app"`
		Error  string `json:"error,omitempty"`
	} `json:"expected"`
}

func (d *testData) ToFixtures(t *testing.T) map[string]mockData {
	fixtures := make(map[string]mockData)
	for _, resp := range d.RemoteMetadata {
		url, err := url.Parse(releaseAPIEndpoint)
		if err != nil {
			t.Fatalf("failed to parse release API endpoint: %v", err)
		}
		query := url.Query()
		query.Set("deviceId", pseudoDeviceID)
		for key, value := range resp.Params {
			query.Set(key, value)
		}
		url.RawQuery = query.Encode()
		fixtures[url.String()] = mockData{
			Metadata:   &resp.Data,
			StatusCode: resp.Code,
		}
	}
	return fixtures
}

func (d *testData) ToUpdateParams() UpdateParams {
	d.UpdateParams.DeviceID = pseudoDeviceID
	return d.UpdateParams
}

func loadTestData(t *testing.T, filename string) *testData {
	f, err := testDataFS.ReadFile(filepath.Join("testdata", "ota", filename))
	if err != nil {
		t.Fatalf("failed to read test data file %s: %v", filename, err)
	}

	var testData testData
	if err := json.Unmarshal(f, &testData); err != nil {
		t.Fatalf("failed to unmarshal test data file %s: %v", filename, err)
	}

	return &testData
}

type mockData struct {
	Metadata   *UpdateMetadata
	StatusCode int
}

type mockHTTPClient struct {
	DoFunc   func(req *http.Request) (*http.Response, error)
	Fixtures map[string]mockData
}

func compareURLs(a *url.URL, b *url.URL) bool {
	if a.String() == b.String() {
		return true
	}
	if a.Host != b.Host || a.Scheme != b.Scheme || a.Path != b.Path {
		return false
	}

	// do a quick check to see if the query parameters are the same
	queryA := a.Query()
	queryB := b.Query()
	if len(queryA) != len(queryB) {
		return false
	}
	for key := range queryA {
		if queryA.Get(key) != queryB.Get(key) {
			return false
		}
	}
	for key := range queryB {
		if queryA.Get(key) != queryB.Get(key) {
			return false
		}
	}
	return true
}

func (m *mockHTTPClient) getFixture(expectedURL *url.URL) *mockData {
	for u, fixture := range m.Fixtures {
		fixtureURL, err := url.Parse(u)
		if err != nil {
			continue
		}
		if compareURLs(fixtureURL, expectedURL) {
			return &fixture
		}
	}
	return nil
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	fixture := m.getFixture(req.URL)
	if fixture == nil {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString("")),
		}, fmt.Errorf("no fixture found for URL: %s", req.URL.String())
	}

	resp := &http.Response{
		StatusCode: fixture.StatusCode,
	}

	jsonData, err := json.Marshal(fixture.Metadata)
	if err != nil {
		return nil, fmt.Errorf("error marshalling metadata: %w", err)
	}

	resp.Body = io.NopCloser(bytes.NewBufferString(string(jsonData)))
	return resp, nil
}

func newMockHTTPClient(fixtures map[string]mockData) *mockHTTPClient {
	return &mockHTTPClient{
		Fixtures: fixtures,
	}
}

func newOtaState(d *testData, t *testing.T) *State {
	pseudoGetLocalVersion := func() (systemVersion *semver.Version, appVersion *semver.Version, err error) {
		appVersion = semver.MustParse(d.LocalMetadata.AppVersion)
		systemVersion = semver.MustParse(d.LocalMetadata.SystemVersion)
		return systemVersion, appVersion, nil
	}

	traceLevel := zerolog.InfoLevel

	if os.Getenv("TEST_LOG_TRACE") == "1" {
		traceLevel = zerolog.TraceLevel
	}
	logger := zerolog.New(os.Stdout).Level(traceLevel)
	otaState := NewState(Options{
		SkipConfirmSystem:  true,
		Logger:             &logger,
		ReleaseAPIEndpoint: releaseAPIEndpoint,
		GetHTTPClient: func() HttpClient {
			if d.RemoteMetadata != nil {
				return newMockHTTPClient(d.ToFixtures(t))
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			if !d.WithoutCerts {
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

func testUsingJson(t *testing.T, filename string) {
	td := loadTestData(t, filename)
	otaState := newOtaState(td, t)
	info, err := otaState.GetUpdateStatus(context.Background(), td.ToUpdateParams())
	if err != nil {
		if td.Expected.Error != "" {
			assert.ErrorContains(t, err, td.Expected.Error)
		} else {
			t.Fatalf("failed to get update status: %v", err)
		}
	}

	if td.Expected.System {
		assert.True(t, info.SystemUpdateAvailable, fmt.Sprintf("system update should available, but reason: %s", info.SystemUpdateAvailableReason))
	} else {
		assert.False(t, info.SystemUpdateAvailable, fmt.Sprintf("system update should not be available, but reason: %s", info.SystemUpdateAvailableReason))
	}

	if td.Expected.App {
		assert.True(t, info.AppUpdateAvailable, fmt.Sprintf("app update should available, but reason: %s", info.AppUpdateAvailableReason))
	} else {
		assert.False(t, info.AppUpdateAvailable, fmt.Sprintf("app update should not be available, but reason: %s", info.AppUpdateAvailableReason))
	}
}

func TestCheckUpdateComponentsSystemOnlyUpgrade(t *testing.T) {
	testUsingJson(t, "system_only_upgrade.json")
}

func TestCheckUpdateComponentsSystemOnlyDowngrade(t *testing.T) {
	testUsingJson(t, "system_only_downgrade.json")
}

func TestCheckUpdateComponentsAppOnlyUpgrade(t *testing.T) {
	testUsingJson(t, "app_only_upgrade.json")
}

func TestCheckUpdateComponentsAppOnlyDowngrade(t *testing.T) {
	testUsingJson(t, "app_only_downgrade.json")
}

func TestCheckUpdateComponentsSystemBothUpgrade(t *testing.T) {
	testUsingJson(t, "both_upgrade.json")
}

func TestCheckUpdateComponentsSystemBothDowngrade(t *testing.T) {
	testUsingJson(t, "both_downgrade.json")
}

func TestCheckUpdateComponentsNoComponents(t *testing.T) {
	testUsingJson(t, "no_components.json")
}

func TestCheckUpdateAppUpgradeWithSignature(t *testing.T) {
	testUsingJson(t, "app_upgrade_with_signature.json")
}

func TestCheckUpdateAppDowngradeNoSignature(t *testing.T) {
	testUsingJson(t, "app_downgrade_no_signature.json")
}
