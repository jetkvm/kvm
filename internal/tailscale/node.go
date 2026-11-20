package tailscale

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/tailscale/wireguard-go/tun"
	"tailscale.com/client/local"
	"tailscale.com/envknob"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

const wireGuardPort = 41641

var logoutTimeout = 10 * time.Second

type Link struct {
	Device   tun.Device
	SetAddrs func(addrs []netip.Addr) error
}

type NodeOptions struct {
	StateDir   string
	Hostname   string
	ControlURL string
	AuthKey    string
	OpenLink   func() (*Link, error)
	Logf       func(format string, args ...any)
	Debugf     func(format string, args ...any)
	OnChange   func(status *Status)
}

type Node struct {
	opts   NodeOptions
	srv    *tsnet.Server
	lc     *local.Client
	link   *Link
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status *Status
	addrs  []netip.Addr
}

func StartNode(opts NodeOptions) (*Node, error) {
	envknob.SetNoLogsNoSupport()

	srv := &tsnet.Server{
		Dir:        opts.StateDir,
		Hostname:   opts.Hostname,
		ControlURL: opts.ControlURL,
		AuthKey:    opts.AuthKey,
		Port:       wireGuardPort,
		UserLogf:   opts.Logf,
		Logf:       opts.Debugf,
	}

	var link *Link
	if opts.OpenLink != nil {
		var err error
		link, err = opts.OpenLink()
		if err != nil {
			return nil, fmt.Errorf("failed to open the tunnel interface: %w", err)
		}
		srv.Tun = link.Device
	}

	if err := srv.Start(); err != nil {
		_ = srv.Close()
		if link != nil {
			_ = link.Device.Close()
		}
		return nil, err
	}

	lc, err := srv.LocalClient()
	if err != nil {
		_ = srv.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{
		opts:   opts,
		srv:    srv,
		lc:     lc,
		link:   link,
		cancel: cancel,
		done:   make(chan struct{}),
		status: &Status{
			Installed:  true,
			Builtin:    true,
			Enabled:    true,
			ControlURL: EffectiveControlURL(opts.ControlURL),
		},
	}
	n.refresh(ctx)
	go n.watch(ctx)

	return n, nil
}

func (n *Node) Status() *Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

func (n *Node) Close() error {
	n.cancel()
	err := n.srv.Close()
	<-n.done
	return err
}

func (n *Node) Logout() error {
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	logoutErr := n.lc.Logout(ctx)
	closeErr := n.Close()
	if logoutErr == nil {
		return closeErr
	}
	return errors.Join(closeErr, os.RemoveAll(n.opts.StateDir))
}

func (n *Node) watch(ctx context.Context) {
	defer close(n.done)

	for ctx.Err() == nil {
		watcher, err := n.lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
		if err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}

		for {
			if _, err := watcher.Next(); err != nil {
				break
			}
			n.refresh(ctx)
		}
		_ = watcher.Close()
	}
}

func (n *Node) refresh(ctx context.Context) {
	st, err := n.lc.StatusWithoutPeers(ctx)
	if err != nil {
		if ctx.Err() == nil {
			n.opts.Logf("failed to read the status: %v", err)
		}
		return
	}
	status := statusFromIPN(st, n.opts.ControlURL)

	n.mu.Lock()
	changed := !reflect.DeepEqual(n.status, status)
	n.status = status
	addrsChanged := !slices.Equal(n.addrs, st.TailscaleIPs)
	n.addrs = st.TailscaleIPs
	n.mu.Unlock()

	if addrsChanged && n.link != nil {
		if err := n.link.SetAddrs(st.TailscaleIPs); err != nil {
			n.opts.Logf("failed to configure the tunnel interface: %v", err)
		}
	}
	if changed && n.opts.OnChange != nil {
		n.opts.OnChange(status)
	}
}

func statusFromIPN(st *ipnstate.Status, controlURL string) *Status {
	status := &Status{
		Installed:    true,
		Builtin:      true,
		Enabled:      true,
		Running:      st.BackendState == ipn.Running.String(),
		BackendState: st.BackendState,
		AuthURL:      st.AuthURL,
		ControlURL:   EffectiveControlURL(controlURL),
		Health:       st.Health,
	}
	if st.CurrentTailnet != nil {
		status.Tailnet = st.CurrentTailnet.Name
	}
	if st.Self != nil {
		ips := make([]string, len(st.TailscaleIPs))
		for i, ip := range st.TailscaleIPs {
			ips[i] = ip.String()
		}
		status.Self = &Peer{
			HostName:     st.Self.HostName,
			DNSName:      st.Self.DNSName,
			TailscaleIPs: ips,
			Online:       st.Self.Online,
			OS:           st.Self.OS,
		}
	}
	return status
}
