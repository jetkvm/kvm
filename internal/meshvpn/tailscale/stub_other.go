//go:build !linux

package tailscale

import (
	"context"
	"errors"

	"github.com/jetkvm/kvm/internal/meshvpn"
)

var errNotSupported = errors.New("tailscale is only supported on Linux")

type HTTPClient interface {
	Get(url string) ([]byte, error)
	Download(url string, dest string, progress meshvpn.ProgressFunc) error
}

type ProviderConfig struct {
	Version    string
	TUNMode    meshvpn.TUNMode
	HTTPClient HTTPClient
}

type Provider struct{}

func NewProvider(_ ProviderConfig) *Provider { return &Provider{} }
func NewDefaultHTTPClient() HTTPClient       { return nil }

func (p *Provider) Name() string               { return "tailscale" }
func (p *Provider) DisplayName() string        { return "Tailscale" }
func (p *Provider) SupportsExitNodes() bool    { return false }
func (p *Provider) SupportsCustomServer() bool { return false }
func (p *Provider) SupportsAuthKey() bool      { return false }

func (p *Provider) Install(_ context.Context, _ meshvpn.ProgressFunc) error {
	return errNotSupported
}

func (p *Provider) Uninstall(_ context.Context) error {
	return errNotSupported
}

func (p *Provider) IsInstalled() bool { return false }

func (p *Provider) Connect(_ context.Context, _ meshvpn.ConnectOptions) (*meshvpn.ConnectResult, error) {
	return nil, errNotSupported
}

func (p *Provider) Disconnect(_ context.Context) error {
	return errNotSupported
}

func (p *Provider) Logout(_ context.Context) error {
	return errNotSupported
}

func (p *Provider) GetStatus(_ context.Context) (*meshvpn.ProviderStatus, error) {
	return &meshvpn.ProviderStatus{
		State:        meshvpn.StateNotInstalled,
		ErrorMessage: errNotSupported.Error(),
	}, nil
}

func (p *Provider) StartStatusMonitor(_ context.Context, _ meshvpn.StatusChangeFunc) {}
func (p *Provider) StopStatusMonitor()                                               {}

func (p *Provider) ListExitNodes(_ context.Context) ([]meshvpn.ExitNode, error) {
	return nil, errNotSupported
}

func (p *Provider) SetExitNode(_ context.Context, _ string, _ bool) error {
	return errNotSupported
}

func (p *Provider) ClearExitNode(_ context.Context) error {
	return errNotSupported
}

func (p *Provider) GetInfo() meshvpn.ProviderInfo {
	return meshvpn.ProviderInfo{
		Name:                 "tailscale",
		DisplayName:          "Tailscale",
		Installed:            false,
		SupportsExitNodes:    false,
		SupportsCustomServer: false,
		SupportsAuthKey:      false,
	}
}

func (p *Provider) GetVersionInfo(_ context.Context) (*meshvpn.VersionInfo, error) {
	return nil, errNotSupported
}

func (p *Provider) Update(_ context.Context, _ string, _ meshvpn.ProgressFunc) error {
	return errNotSupported
}

func (p *Provider) GetTUNMode() meshvpn.TUNMode                           { return meshvpn.TUNModeUserspace }
func (p *Provider) SetTUNMode(_ context.Context, _ meshvpn.TUNMode) error { return errNotSupported }
func (p *Provider) SetAdvertiseExitNode(_ context.Context, _ bool) error  { return errNotSupported }
