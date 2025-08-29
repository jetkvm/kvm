package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/usbgadget"
)

func onHidMessage(data []byte, session *Session) {
	if len(data) < 1 {
		logger.Warn().Int("length", len(data)).Msg("received empty data in HID RPC message handler")
		return
	}

	var (
		message hidrpc.Message
		rpcErr  error
	)

	if err := hidrpc.Unmarshal(data, &message); err != nil {
		logger.Warn().Err(err).Msg("failed to unmarshal HID RPC message")
		return
	}

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
		session.hidRpcAvailable = true
	case hidrpc.TypeKeypressReport, hidrpc.TypeKeyboardReport:
		keysDownState, err := handleHidRpcKeyboardInput(message)
		if keysDownState != nil {
			reportHidRpcKeysDownState(*keysDownState, session)
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

func handleHidRpcKeyboardInput(message hidrpc.Message) (*usbgadget.KeysDownState, error) {
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

func reportHidRpc(params any, session *Session) {
	var (
		message []byte
		err     error
	)
	switch params := params.(type) {
	case usbgadget.KeyboardState:
		message, err = hidrpc.NewKeyboardLedMessage(params).Marshal()
	case usbgadget.KeysDownState:
		message, err = hidrpc.NewKeydownStateMessage(params).Marshal()
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

func reportHidRpcKeyboardLedState(state usbgadget.KeyboardState, session *Session) {
	if !session.hidRpcAvailable {
		writeJSONRPCEvent("keyboardLedState", state, currentSession)
	}
	reportHidRpc(state, session)
}

func reportHidRpcKeysDownState(state usbgadget.KeysDownState, session *Session) {
	if !session.hidRpcAvailable {
		writeJSONRPCEvent("keysDownState", state, currentSession)
	}
	reportHidRpc(state, session)
}
