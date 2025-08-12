package kvm

import (
	"encoding/binary"

	"github.com/pion/webrtc/v4"
)

type HidPayloadType uint8

const (
	HidPayloadTypeKeyboardReport           HidPayloadType = 1
	HidPayloadTypeKeyboardReportNoModifier HidPayloadType = 2
	HidPayloadTypeKeyboardReportNothing    HidPayloadType = 3
	HidPayloadTypeAbsMouseReport           HidPayloadType = 8
	HidPayloadTypeRelMouseReport           HidPayloadType = 9
)

func handleHidMessage(msg webrtc.DataChannelMessage) {
	if msg.IsString {
		webrtcLogger.Info().Interface("msg", msg.Data).Msg("Hid message is a string, skipping")
		return
	}

	payloadType := HidPayloadType(msg.Data[0])
	switch payloadType {
	case HidPayloadTypeKeyboardReport:
		if len(msg.Data) < 3 {
			return
		}
		modifier := msg.Data[1]
		keys := msg.Data[2:]
		_ = gadget.KeyboardReport(modifier, keys)
	case HidPayloadTypeKeyboardReportNoModifier:
		if len(msg.Data) < 2 {
			return
		}
		keys := msg.Data[1:]
		_ = gadget.KeyboardReport(0, keys)
	case HidPayloadTypeKeyboardReportNothing:
		_ = gadget.KeyboardReport(0, []byte{})
	case HidPayloadTypeAbsMouseReport:
		if len(msg.Data) < 6 {
			return
		}
		x := binary.LittleEndian.Uint16(msg.Data[1:3])
		y := binary.LittleEndian.Uint16(msg.Data[3:5])
		buttons := msg.Data[5]
		webrtcLogger.Info().Uint16("x", x).Uint16("y", y).Uint8("buttons", buttons).Msg("Absolute mouse report")
		_ = gadget.AbsMouseReport(int(x), int(y), buttons)
	case HidPayloadTypeRelMouseReport:
		if len(msg.Data) < 4 {
			return
		}
		dx := int8(msg.Data[1])
		dy := int8(msg.Data[2])
		buttons := msg.Data[3]
		webrtcLogger.Info().Int8("dx", dx).Int8("dy", dy).Uint8("buttons", buttons).Msg("Relative mouse report")
		_ = gadget.RelMouseReport(dx, dy, buttons)
	}
}
