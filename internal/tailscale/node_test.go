package tailscale

import (
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tailscale/wireguard-go/tun/tuntest"
	"tailscale.com/net/netns"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

type testLink struct {
	mu    sync.Mutex
	addrs []netip.Addr
}

func (l *testLink) open() (*Link, error) {
	return &Link{
		Device: tuntest.NewChannelTUN().TUN(),
		SetAddrs: func(addrs []netip.Addr) error {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.addrs = addrs
			return nil
		},
	}, nil
}

func (l *testLink) current() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.addrs))
	for i, addr := range l.addrs {
		out[i] = addr.String()
	}
	return out
}

func startTestControl(t *testing.T) *testcontrol.Server {
	t.Helper()
	netns.SetEnabled(false)
	t.Cleanup(func() { netns.SetEnabled(true) })

	control := &testcontrol.Server{
		DERPMap:     integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		RequireAuth: true,
		Logf:        logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	return control
}

func startTestNode(t *testing.T, control *testcontrol.Server, stateDir, authKey string, link *testLink) *Node {
	t.Helper()
	node, err := StartNode(NodeOptions{
		StateDir:   stateDir,
		Hostname:   "jetkvm-test",
		ControlURL: control.HTTPTestServer.URL,
		AuthKey:    authKey,
		OpenLink:   link.open,
		Logf:       t.Logf,
		Debugf:     logger.Discard,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	return node
}

func waitForStatus(t *testing.T, node *Node, what string, match func(*Status) bool) *Status {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		status := node.Status()
		if match(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; last status: %+v", what, status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func needsLogin(status *Status) bool {
	return status.BackendState == "NeedsLogin" && status.AuthURL != ""
}

func running(status *Status) bool {
	return status.Running && status.Self != nil && len(status.Self.TailscaleIPs) > 0
}

func login(t *testing.T, control *testcontrol.Server, node *Node) *Status {
	t.Helper()
	pending := waitForStatus(t, node, "a login URL", needsLogin)
	require.True(t, control.CompleteAuth(pending.AuthURL), "control did not issue %s", pending.AuthURL)
	return waitForStatus(t, node, "the node to connect", running)
}

func TestNodeJoinsTailnetAfterLoginURLIsApproved(t *testing.T) {
	control := startTestControl(t)
	link := &testLink{}

	var mu sync.Mutex
	var notified []*Status
	node, err := StartNode(NodeOptions{
		StateDir:   t.TempDir(),
		Hostname:   "jetkvm-test",
		ControlURL: control.HTTPTestServer.URL,
		OpenLink:   link.open,
		Logf:       t.Logf,
		Debugf:     logger.Discard,
		OnChange: func(status *Status) {
			mu.Lock()
			defer mu.Unlock()
			notified = append(notified, status)
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })

	pending := waitForStatus(t, node, "a login URL", needsLogin)
	assert.False(t, pending.Running)
	assert.Contains(t, pending.AuthURL, control.HTTPTestServer.URL)
	assert.Empty(t, link.current())

	require.True(t, control.CompleteAuth(pending.AuthURL))
	connected := waitForStatus(t, node, "the node to connect", running)

	assert.Equal(t, "jetkvm-test", connected.Self.HostName)
	assert.Equal(t, control.HTTPTestServer.URL, connected.ControlURL)
	assert.Empty(t, connected.AuthURL)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(connected.Self.TailscaleIPs, link.current())
	}, 5*time.Second, 20*time.Millisecond, "the tunnel interface did not get the tailnet addresses")

	require.Len(t, control.AllNodes(), 1)
	assert.Equal(t, control.AllNodes()[0].Addresses[0].Addr().String(), connected.Self.TailscaleIPs[0])

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, notified)
	assert.True(t, notified[len(notified)-1].Running, "the last notification must report the connected state")
}

func TestNodeJoinsTailnetWithAuthKey(t *testing.T) {
	control := startTestControl(t)
	control.RequireAuth = false
	control.RequireAuthKey = "tskey-auth-test"

	rejected := startTestNode(t, control, t.TempDir(), "tskey-auth-wrong", &testLink{})
	waitForStatus(t, rejected, "the wrong key to be refused", func(status *Status) bool {
		return status.BackendState == "NeedsLogin"
	})
	assert.Empty(t, control.AllNodes())
	require.NoError(t, rejected.Close())

	node := startTestNode(t, control, t.TempDir(), "tskey-auth-test", &testLink{})
	waitForStatus(t, node, "the node to connect", running)
	assert.Len(t, control.AllNodes(), 1)
}

func TestNodeKeepsIdentityAcrossRestart(t *testing.T) {
	control := startTestControl(t)
	stateDir := t.TempDir()

	first := startTestNode(t, control, stateDir, "", &testLink{})
	before := login(t, control, first)
	require.NoError(t, first.Close())

	link := &testLink{}
	second := startTestNode(t, control, stateDir, "", link)
	after := waitForStatus(t, second, "the node to reconnect without a login", running)

	assert.Equal(t, before.Self.TailscaleIPs, after.Self.TailscaleIPs)
	assert.Len(t, control.AllNodes(), 1)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(after.Self.TailscaleIPs, link.current())
	}, 5*time.Second, 20*time.Millisecond)
}

func TestNodeLogoutRequiresNewLogin(t *testing.T) {
	control := startTestControl(t)
	stateDir := t.TempDir()

	first := startTestNode(t, control, stateDir, "", &testLink{})
	login(t, control, first)
	require.NoError(t, first.Logout())

	second := startTestNode(t, control, stateDir, "", &testLink{})
	pending := waitForStatus(t, second, "a login URL after logout", needsLogin)
	assert.False(t, pending.Running)
}

func TestNodeLogoutForgetsIdentityWhenControlIsUnreachable(t *testing.T) {
	previous := logoutTimeout
	logoutTimeout = time.Second
	t.Cleanup(func() { logoutTimeout = previous })

	control := startTestControl(t)
	stateDir := t.TempDir()

	first := startTestNode(t, control, stateDir, "", &testLink{})
	login(t, control, first)

	control.HTTPTestServer.CloseClientConnections()
	control.HTTPTestServer.Close()
	require.NoError(t, first.Logout())

	replacement := startTestControl(t)
	replacement.RequireAuth = false
	second := startTestNode(t, replacement, stateDir, "", &testLink{})
	waitForStatus(t, second, "the node to register again", running)

	require.Len(t, replacement.AllNodes(), 1)
	assert.NotEqual(t, control.AllNodes()[0].Key, replacement.AllNodes()[0].Key,
		"the node must not reuse the key of the session it logged out of")
}
