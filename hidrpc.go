package kvm

import (
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/usbgadget"
)

func handleHidRPCMessage(message hidrpc.Message, session *Session) {
	var rpcErr error

	switch message.Type() {
	case hidrpc.TypeHandshake:
		message, err := hidrpc.NewHandshakeMessage().Marshal()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to marshal handshake message")
			return
		}
		if err := session.HidChannel.Send(message); err != nil {
			logger.Warn().Err(err).Msg("failed to send handshake message")
			return
		}
		session.hidRPCAvailable = true
	case hidrpc.TypeKeypressReport, hidrpc.TypeKeyboardReport:
		keysDownState, err := handleHidRPCKeyboardInput(message)
		if keysDownState != nil {
			session.reportHidRPCKeysDownState(*keysDownState)
		}
		rpcErr = err
	case hidrpc.TypePointerReport:
		pointerReport, err := message.PointerReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get pointer report")
			return
		}
		rpcErr = rpcAbsMouseReport(pointerReport.X, pointerReport.Y, pointerReport.Button)
	case hidrpc.TypeMouseReport:
		mouseReport, err := message.MouseReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get mouse report")
			return
		}
		rpcErr = rpcRelMouseReport(mouseReport.DX, mouseReport.DY, mouseReport.Button)
	default:
		logger.Warn().Uint8("type", uint8(message.Type())).Msg("unknown HID RPC message type")
	}

	if rpcErr != nil {
		logger.Warn().Err(rpcErr).Msg("failed to handle HID RPC message")
	}
}

func onHidMessage(data []byte, session *Session) {
	scopedLogger := hidRPCLogger.With().Bytes("data", data).Logger()
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

	scopedLogger = scopedLogger.With().Str("descr", message.String()).Logger()

	t := time.Now()

	r := make(chan interface{})
	go func() {
		handleHidRPCMessage(message, session)
		r <- nil
	}()
	select {
	case <-time.After(1 * time.Second):
		scopedLogger.Warn().Msg("HID RPC message timed out")
	case <-r:
		scopedLogger.Debug().Dur("duration", time.Since(t)).Msg("HID RPC message handled")
	}
}

func handleHidRPCKeyboardInput(message hidrpc.Message) (*usbgadget.KeysDownState, error) {
	switch message.Type() {
	case hidrpc.TypeKeypressReport:
		keypressReport, err := message.KeypressReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get keypress report")
			return nil, err
		}
		keysDownState, rpcError := rpcKeypressReport(keypressReport.Key, keypressReport.Press)
		return &keysDownState, rpcError
	case hidrpc.TypeKeyboardReport:
		keyboardReport, err := message.KeyboardReport()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to get keyboard report")
			return nil, err
		}
		keysDownState, rpcError := rpcKeyboardReport(keyboardReport.Modifier, keyboardReport.Keys)
		return &keysDownState, rpcError
	}

	return nil, fmt.Errorf("unknown HID RPC message type: %d", message.Type())
}

func reportHidRPC(params any, session *Session) {
	if session == nil {
		logger.Warn().Msg("session is nil, skipping reportHidRPC")
		return
	}

	if !session.hidRPCAvailable || session.HidChannel == nil {
		logger.Warn().Msg("HID RPC is not available, skipping reportHidRPC")
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
	default:
		err = fmt.Errorf("unknown HID RPC message type: %T", params)
	}

	if err != nil {
		logger.Warn().Err(err).Msg("failed to marshal HID RPC message")
		return
	}

	if message == nil {
		logger.Warn().Msg("failed to marshal HID RPC message")
		return
	}

	if err := session.HidChannel.Send(message); err != nil {
		logger.Warn().Err(err).Msg("failed to send HID RPC message")
	}
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
