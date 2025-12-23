package kvm

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/jetkvm/kvm/internal/utils"
	"github.com/rs/zerolog"
)

func handleHidRPCMessage(message hidrpc.Message, session *Session) error {
	switch message.Type() {
	case hidrpc.TypeHandshake:
		return handleHidRPCHandshake(session)
	case hidrpc.TypeKeyboardMacroReport:
		return handleKeyboardMacro(message)
	case hidrpc.TypeKeypressReport, hidrpc.TypeKeyboardReport:
		return handleHidRPCKeyboardInput(message)
	case hidrpc.TypeCancelKeyboardMacroReport:
		return rpcCancelKeyboardMacro()
	case hidrpc.TypeKeypressKeepAliveReport:
		return handleHidRPCKeypressKeepAlive(session)
	case hidrpc.TypePointerReport:
		return handlePointerReport(message)
	case hidrpc.TypeMouseReport:
		return handleMouseReport(message)
	}

	return fmt.Errorf("unknown HID RPC message type %d", message.Type())
}

func handleHidRPCHandshake(session *Session) error {
	hidRPCLogger.Debug().Msg("handling handshake")
	message, err := hidrpc.NewHandshakeMessage().Marshal()
	if err != nil {
		return err
	}
	if err = session.HidChannel.Send(message); err != nil {
		return err
	}
	session.hidRPCAvailable = true
	return nil
}

func handleKeyboardMacro(message hidrpc.Message) error {
	keyboardMacroReport, err := message.KeyboardMacroReport()
	if err != nil {
		return err
	}
	hidRPCLogger.Debug().Interface("keyboardMacroReport", keyboardMacroReport).Msg("handling keyboard macro")
	return rpcExecuteKeyboardMacro(keyboardMacroReport.Steps)
}

func handleMouseReport(message hidrpc.Message) error {
	mouseReport, err := message.MouseReport()
	if err != nil {
		return err
	}
	hidRPCLogger.Debug().Interface("mouseReport", mouseReport).Msg("handling relative mouse")
	return rpcRelMouseReport(mouseReport.DX, mouseReport.DY, mouseReport.Button)
}

func handlePointerReport(message hidrpc.Message) error {
	pointerReport, err := message.PointerReport()
	if err != nil {
		return err
	}
	hidRPCLogger.Debug().Interface("pointerReport", pointerReport).Msg("handling absolute pointer")
	return rpcAbsMouseReport(pointerReport.X, pointerReport.Y, pointerReport.Button)
}

func onHidMessage(msg hidQueueMessage, session *Session, index int) {
	logger := hidRPCLogger.With().Int("queueIndex", index).Str("channel", msg.channel).Logger()
	data := msg.Data

	if logger.GetLevel() <= zerolog.TraceLevel {
		logger.Trace().Object("data", utils.ByteSlice(data)).Msg("HID RPC message received")
	}

	if len(data) < 1 {
		logger.Warn().Int("length", len(data)).Msg("received empty data in HID RPC message handler")
		return
	}

	var message hidrpc.Message

	if err := hidrpc.Unmarshal(data, &message); err != nil {
		logger.Warn().Err(err).Msg("failed to unmarshal HID RPC message")
		return
	}

	if logger.GetLevel() <= zerolog.DebugLevel {
		logger = logger.With().Str("descr", message.String()).Logger()
	}

	t := time.Now()
	r := make(chan interface{})
	go func() {
		r <- handleHidRPCMessage(message, session)
	}()
	select {
	case <-time.After(1 * time.Second):
		logger.Warn().Msg("HID RPC message took too long")
	case err := <-r:
		logger.Debug().Dur("duration", time.Since(t)).Msg("HID RPC message handled")
		if err != nil {
			logger.Warn().Err(err.(error)).Msg("failed to handle HID RPC message")
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
const maxStaleness = 225 * time.Millisecond      // discard ancient packets outright

func handleHidRPCKeypressKeepAlive(session *Session) error {
	session.keepAliveJitterLock.Lock()
	defer session.keepAliveJitterLock.Unlock()

	now := time.Now()

	// 1) Staleness guard: ensures packets that arrive far beyond the life of a valid key hold
	// (e.g. after a network stall, retransmit burst, or machine sleep) are ignored outright.
	// This prevents “zombie” keepalives from reviving a key that should already be released.
	if !session.lastTimerResetTime.IsZero() && now.Sub(session.lastTimerResetTime) > maxStaleness {
		return nil
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
				return nil
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
	return nil
}

func handleHidRPCKeyboardInput(message hidrpc.Message) error {
	logger := hidRPCLogger.With().Object("message", message).Logger()

	switch message.Type() {
	case hidrpc.TypeKeypressReport:
		keypressReport, err := message.KeypressReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get keypress report")
			return err
		}
		logger.Debug().Object("keypressReport", keypressReport).Msg("handling key press")
		return rpcKeypressReport(keypressReport.Key, keypressReport.Press)
	case hidrpc.TypeKeyboardReport:
		keyboardReport, err := message.KeyboardReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get keyboard report")
			return err
		}
		logger.Debug().Object("keyboardReport", keyboardReport).Msg("handling keyboard")
		return rpcKeyboardReport(keyboardReport.Modifier, keyboardReport.Keys)
	}

	return fmt.Errorf("unknown HID RPC message type: %d", message.Type())
}

func reportHidRPC(params any, session *Session) {
	logger := hidRPCLogger.With().Interface("params", params).Logger()

	if session == nil {
		logger.Warn().Msg("session is nil, skipping reportHidRPC")
		return
	}

	if !session.hidRPCAvailable || session.HidChannel == nil {
		logger.Warn().
			Bool("hidRPCAvailable", session.hidRPCAvailable).
			Bool("HidChannel", session.HidChannel != nil).
			Msg("HID RPC is not available, skipping reportHidRPC")
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

	logger = logger.With().Type("type", params).Logger()

	if err != nil || message == nil {
		logger.Warn().Err(err).Msg("failed to marshal HID RPC message")
		return
	}

	// fire and forget...
	go func() {
		t := time.Now()
		r := make(chan interface{})
		go func() {
			logger.Trace().Msg("sending HID RPC report")
			r <- session.HidChannel.Send(message)
		}()
		select {
		case <-time.After(1 * time.Second):
			logger.Warn().Msg("HID RPC report took too long")
		case err := <-r:
			logger.Debug().Dur("duration", time.Since(t)).Msg("HID RPC report sent")
			if err != nil {
				if errors.Is(err.(error), io.ErrClosedPipe) {
					logger.Warn().Err(err.(error)).Msg("HID RPC channel closed, dropping reportHidRPC")
					return
				}
				logger.Warn().Err(err.(error)).Msg("failed to send HID RPC report")
			}
		}
	}()
}

func (s *Session) reportHidRPCKeyboardLedState(state usbgadget.KeyboardState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardLedState", state, s)
	}
	reportHidRPC(state, s)
}

func (s *Session) reportHidRPCKeysDownState(state usbgadget.KeysDownState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keysDownState", state, s)
	}
	reportHidRPC(state, s)
}

func (s *Session) reportHidRPCKeyboardMacroState(state hidrpc.KeyboardMacroState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardMacroState", state, s)
	}
	reportHidRPC(state, s)
}
