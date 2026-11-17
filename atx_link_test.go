package kvm

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	at      time.Duration
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

func (c *fakeClock) afterFunc(d time.Duration, fn func()) atxTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now + d, fn: fn}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []func()
	for _, t := range c.timers {
		if !t.stopped && t.at <= c.now {
			t.stopped = true
			due = append(due, t.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped {
			n++
		}
	}
	return n
}

func useFakeATXClock(t *testing.T) *fakeClock {
	t.Helper()
	clock := &fakeClock{}
	original := atxAfterFunc
	atxAfterFunc = clock.afterFunc
	t.Cleanup(func() { atxAfterFunc = original })
	return clock
}

type fakeToken struct{ done chan struct{} }

func (t fakeToken) Wait() bool                     { return true }
func (t fakeToken) WaitTimeout(time.Duration) bool { return true }
func (t fakeToken) Done() <-chan struct{}          { return t.done }
func (t fakeToken) Error() error                   { return nil }

type fakeMQTTClient struct {
	mqtt.Client

	hold func(ATXState)

	mu        sync.Mutex
	published []ATXState
}

func (c *fakeMQTTClient) IsConnected() bool { return true }

func (c *fakeMQTTClient) Publish(topic string, _ byte, _ bool, payload any) mqtt.Token {
	done := make(chan struct{})
	close(done)
	if topic != "jetkvm/test/atx/state" {
		return fakeToken{done}
	}
	var state ATXState
	if err := json.Unmarshal(payload.([]byte), &state); err != nil {
		panic(err)
	}
	if c.hold != nil {
		c.hold(state)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.published = append(c.published, state)
	return fakeToken{done}
}

func (c *fakeMQTTClient) states() []ATXState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ATXState(nil), c.published...)
}

func newTestMQTTManager(debounce time.Duration) (*MQTTManager, *fakeMQTTClient) {
	client := &fakeMQTTClient{}
	m := &MQTTManager{
		client:     client,
		baseTopic:  "jetkvm/test",
		debounceMs: int(debounce / time.Millisecond),
		done:       make(chan struct{}),
	}
	m.connected.Store(true)
	return m, client
}

func useTestMQTTManager(t *testing.T) *fakeMQTTClient {
	t.Helper()
	m, client := newTestMQTTManager(0)
	original := mqttManager
	mqttManager = m
	t.Cleanup(func() { mqttManager = original })
	return client
}

func deliverATXLine(t *testing.T, port *fakeSerialPort, client *fakeMQTTClient, line string) ATXState {
	t.Helper()
	before := len(client.states())
	port.deliver(t, line)
	eventually(t, "the ATX state of line "+line, func() bool { return len(client.states()) > before })
	states := client.states()
	return states[len(states)-1]
}

func respondingNow(t *testing.T) bool {
	t.Helper()
	state, err := rpcGetATXState()
	if err != nil {
		t.Fatal(err)
	}
	return state.Responding
}

func TestATXBoardSilenceFlipsRespondingOnce(t *testing.T) {
	clock := useFakeATXClock(t)
	client := useTestMQTTManager(t)
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}

	clock.advance(atxSilenceTimeout - time.Second)
	if !respondingNow(t) {
		t.Fatal("board reported as silent before the timeout after activation")
	}
	if got := client.states(); len(got) != 0 {
		t.Fatalf("published %v before any line or timeout", got)
	}

	clock.advance(time.Second)
	if respondingNow(t) {
		t.Fatal("board still reported as responding after the timeout without a line")
	}
	clock.advance(5 * atxSilenceTimeout)
	got := client.states()
	if len(got) != 1 || got[0].Responding {
		t.Fatalf("want one published state with responding=false, got %v", got)
	}

	state := deliverATXLine(t, ports.current(), client, "1011\n")
	if !state.Responding || !state.Power {
		t.Fatalf("state after a new line = %+v, want power on and responding", state)
	}
	if !respondingNow(t) {
		t.Fatal("getATXState still reports silence after a new line")
	}
}

func TestATXBoardLinesKeepItResponding(t *testing.T) {
	clock := useFakeATXClock(t)
	client := useTestMQTTManager(t)
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	port := ports.current()

	for range 30 {
		clock.advance(atxSilenceTimeout - time.Second)
		if state := deliverATXLine(t, port, client, "1111\n"); !state.Responding {
			t.Fatalf("state %+v reports silence while lines arrive", state)
		}
	}
	if !respondingNow(t) {
		t.Fatal("board reported as silent while lines arrive")
	}

	clock.advance(atxSilenceTimeout)
	if respondingNow(t) {
		t.Fatal("board still reported as responding a full timeout after the last line")
	}
}

func TestATXSwitchAwayStopsSilenceTimer(t *testing.T) {
	clock := useFakeATXClock(t)
	client := useTestMQTTManager(t)
	useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	clock.advance(atxSilenceTimeout / 2)
	if err := extPort.activate("dc-power"); err != nil {
		t.Fatal(err)
	}
	if n := clock.pending(); n != 0 {
		t.Fatalf("%d silence timers still pending after switching to dc-power", n)
	}
	clock.advance(10 * atxSilenceTimeout)
	if got := client.states(); len(got) != 0 {
		t.Fatalf("ATX state published after switching away: %v", got)
	}
	if !respondingNow(t) {
		t.Fatal("board reported as silent after switching away")
	}
}

func TestATXSwitchAwayClearsSilence(t *testing.T) {
	clock := useFakeATXClock(t)
	useTestMQTTManager(t)
	useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	clock.advance(atxSilenceTimeout)
	if respondingNow(t) {
		t.Fatal("board not reported as silent after the timeout")
	}
	if err := extPort.activate(""); err != nil {
		t.Fatal(err)
	}
	if !respondingNow(t) {
		t.Fatal("silence of the old ATX board is still reported after switching away")
	}

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	clock.advance(atxSilenceTimeout - time.Second)
	if !respondingNow(t) {
		t.Fatal("board reported as silent before the timeout after reactivation")
	}
}

func TestATXBlockedPublishDoesNotHoldTheLink(t *testing.T) {
	clock := useFakeATXClock(t)
	client := useTestMQTTManager(t)
	ports := useFakeExtensionPort(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	client.hold = func(state ATXState) {
		if !state.Responding {
			close(entered)
			<-release
		}
	}

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	go clock.advance(atxSilenceTimeout)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the silence was not published")
	}

	ports.current().deliver(t, "1111\n")
	eventually(t, "a line to re-arm the timer during a blocked publish", func() bool {
		return clock.pending() == 1 && respondingNow(t)
	})

	extPort.mu.Lock()
	link := extPort.atx
	extPort.mu.Unlock()
	stopped := make(chan struct{})
	go func() {
		link.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop waited for a blocked publish")
	}
	if n := clock.pending(); n != 0 {
		t.Fatalf("%d silence timers still pending after stop", n)
	}
}

func TestMQTTPublishesRespondingChangeWithDebounce(t *testing.T) {
	m, client := newTestMQTTManager(time.Hour)

	m.publishATXState(ATXState{Power: true, HDD: true, Responding: true})
	m.publishATXState(ATXState{Power: true, HDD: false, Responding: true})
	m.publishATXState(ATXState{Power: true, HDD: false, Responding: false})
	m.publishATXState(ATXState{Power: true, HDD: false, Responding: false})
	m.publishATXState(ATXState{Power: true, HDD: false, Responding: true})

	want := []ATXState{
		{Power: true, HDD: true, Responding: true},
		{Power: true, HDD: false, Responding: false},
		{Power: true, HDD: false, Responding: true},
	}
	got := client.states()
	if len(got) != len(want) {
		t.Fatalf("published %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("published %v, want %v", got, want)
		}
	}
}
