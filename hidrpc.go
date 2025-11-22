package kvm

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

func handleHidRPCMessage(message hidrpc.Message, session *Session) error {
	var err error

	switch message.Type() {
	case hidrpc.TypeHandshake:
		if message, err := hidrpc.NewHandshakeMessage().Marshal(); err == nil {
			if err = session.HidChannel.Send(message); err == nil {
				session.hidRPCAvailable = true
				return nil
			}
		}

	case hidrpc.TypeKeypressReport, hidrpc.TypeKeyboardReport:
		return handleHidRPCKeyboardInput(message)

	case hidrpc.TypeKeyboardMacroReport:
		if keyboardMacroReport, err := message.KeyboardMacroReport(); err == nil {
			return rpcExecuteKeyboardMacro(keyboardMacroReport.Steps)
		}
	case hidrpc.TypeCancelKeyboardMacroReport:
		rpcCancelKeyboardMacro()
		return nil

	case hidrpc.TypeKeypressKeepAliveReport:
		handleHidRPCKeypressKeepAlive(session)
		return nil

	case hidrpc.TypePointerReport:
		if pointerReport, err := message.PointerReport(); err == nil {
			return rpcAbsMouseReport(pointerReport.X, pointerReport.Y, pointerReport.Button)
		}

	case hidrpc.TypeMouseReport:
		if mouseReport, err := message.MouseReport(); err == nil {
			return rpcRelMouseReport(mouseReport.DX, mouseReport.DY, mouseReport.Button)
		}
	}

	return fmt.Errorf("unknown HID RPC message type %d err %w", message.Type(), err)
}

func onHidMessage(msg hidQueueMessage, session *Session) {
	data := msg.Data

	scopedLogger := logging.GetSubsystemLogger("hidrpc").
		With().
		Str("channel", msg.channel).
		Bytes("data", data).
		Logger()
	scopedLogger.Debug().Msg("HID RPC message received")

	if len(data) < 1 {
		scopedLogger.Warn().Int("length", len(data)).Msg("received empty data in HID RPC message handler")
		return
	}

	var message hidrpc.Message

	if err := hidrpc.Unmarshal(data, &message); err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to unmarshal HID RPC message")
		return
	}

	if scopedLogger.GetLevel() <= zerolog.DebugLevel {
		scopedLogger = scopedLogger.With().Str("descr", message.String()).Logger()
	}

	t := time.Now()
	r := make(chan interface{})
	go func() {
		r <- handleHidRPCMessage(message, session)
	}()
	select {
	case <-time.After(1 * time.Second):
		scopedLogger.Warn().Msg("HID RPC message timed out")
	case err := <-r:
		scopedLogger.Debug().Dur("duration", time.Since(t)).Msg("HID RPC message handled")
		if err != nil {
			scopedLogger.Warn().Err(err.(error)).Msg("failed to handle HID RPC message")
		}
	}
}

// Tunables
// Keep in mind
// macOS default: 15 * 15 = 225ms https://discussions.apple.com/thread/1316947?sortBy=rank
// Linux default: 250ms https://man.archlinux.org/man/kbdrate.8.en
// Windows default: 1s `HKEY_CURRENT_USER\Control Panel\Accessibility\Keyboard Response\AutoRepeatDelay`

const expectedRate = 50 * time.Millisecond       // expected keepalive interval
const maxLateness = 50 * time.Millisecond        // max jitter we'll tolerate OR jitter budget
const baseExtension = expectedRate + maxLateness // 100ms extension on perfect tick

const maxStaleness = 225 * time.Millisecond // discard ancient packets outright

func handleHidRPCKeypressKeepAlive(session *Session) {
	session.keepAliveJitterLock.Lock()
	defer session.keepAliveJitterLock.Unlock()

	now := time.Now()

	// 1) Staleness guard: ensures packets that arrive far beyond the life of a valid key hold
	// (e.g. after a network stall, retransmit burst, or machine sleep) are ignored outright.
	// This prevents “zombie” keepalives from reviving a key that should already be released.
	if !session.lastTimerResetTime.IsZero() && now.Sub(session.lastTimerResetTime) > maxStaleness {
		return
	}

	timerExtension := baseExtension

	if !session.lastKeepAliveArrivalTime.IsZero() {
		timeSinceLastTick := now.Sub(session.lastKeepAliveArrivalTime)
		lateness := timeSinceLastTick - expectedRate

		if lateness > 0 {
			if lateness <= maxLateness {
				// --- Small lateness (within jitterBudget) ---
				// This is normal jitter (e.g., Wi-Fi contention).
				// We still accept the tick, but *reduce the extension*
				// so that the total hold time stays aligned with REAL client side intent.
				timerExtension -= lateness
			} else {
				// --- Large lateness (beyond jitterBudget) ---
				// This is likely a retransmit stall or ordering delay.
				// We reject the tick entirely and DO NOT extend,
				// so the auto-release still fires on time.
				return
			}
		}
	}

	// Only valid ticks update our state and extend the timer.
	session.lastKeepAliveArrivalTime = now
	session.lastTimerResetTime = now
	if gadget != nil {
		gadget.DelayAutoReleaseWithDuration(timerExtension)
	}

	// On a miss: do not advance any state — keeps baseline stable.
}

func handleHidRPCKeyboardInput(message hidrpc.Message) error {
	switch message.Type() {
	case hidrpc.TypeKeypressReport:
		keypressReport, err := message.KeypressReport()
		if err != nil {
			logging.GetSubsystemLogger("hidrpc").Warn().Err(err).Msg("failed to get keypress report")
			return err
		}
		return rpcKeypressReport(keypressReport.Key, keypressReport.Press)

	case hidrpc.TypeKeyboardReport:
		keyboardReport, err := message.KeyboardReport()
		if err != nil {
			logging.GetSubsystemLogger("hidrpc").Warn().Err(err).Msg("failed to get keyboard report")
			return err
		}
		return rpcKeyboardReport(keyboardReport.Modifier, keyboardReport.Keys)
	}

	return fmt.Errorf("unknown HID RPC message type: %d", message.Type())
}

func reportHidRPC(params any, session *Session) {
	if session == nil {
		logging.GetSubsystemLogger("hidrpc").Warn().Msg("session is nil")
		return
	}

	if !session.hidRPCAvailable {
		logging.GetSubsystemLogger("hidrpc").Warn().Msg("HID RPC is not available")
		return
	}

	channel := session.HidChannel

	if channel == nil || channel.ReadyState() != webrtc.DataChannelStateOpen {
		logging.GetSubsystemLogger("hidrpc").Warn().Msg("HID RPC channel is not open")
		return
	}

	var (
		message []byte
		err     error
	)
	switch params := params.(type) {
	case usbgadget.KeyboardState:
		message, err = hidrpc.NewKeyboardLedMessage(params).Marshal()
	case usbgadget.KeysDownState:
		message, err = hidrpc.NewKeydownStateMessage(params).Marshal()
	case hidrpc.KeyboardMacroState:
		message, err = hidrpc.NewKeyboardMacroStateMessage(params.State, params.IsPaste).Marshal()
	default:
		err = fmt.Errorf("unknown HID RPC message type: %T", params)
	}

	if err != nil {
		logging.GetSubsystemLogger("hidrpc").Warn().Err(err).Msg("failed to marshal HID RPC message")
		return
	}

	if message == nil {
		logging.GetSubsystemLogger("hidrpc").Warn().Msg("failed to marshal HID RPC message")
		return
	}

	// fire and forget...
	go func() {
		if err := channel.Send(message); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				logging.GetSubsystemLogger("hidrpc").Debug().Err(err).Msg("HID RPC channel closed")
				return
			}
			logging.GetSubsystemLogger("hidrpc").Warn().Err(err).Msg("failed to send HID RPC message")
		}
	}()
}

func (s *Session) reportHidRPCKeyboardLedState(state usbgadget.KeyboardState) {
	logging.GetSubsystemLogger("hidrpc").Debug().Object("state", state).Msg("reporting keyboard led state")
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardLedState", state, s)
	}
	reportHidRPC(state, s)
}

func (s *Session) reportHidRPCKeysDownState(state usbgadget.KeysDownState) {
	logging.GetSubsystemLogger("hidrpc").Debug().Object("state", state).Msg("reporting keys down state")
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keysDownState", state, s)
	}
	reportHidRPC(state, s)
}

func (s *Session) reportHidRPCKeyboardMacroState(state hidrpc.KeyboardMacroState) {
	logging.GetSubsystemLogger("hidrpc").Debug().Object("state", state).Msg("reporting keyboard macro state")
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardMacroState", state, s)
	}
	reportHidRPC(state, s)
}
