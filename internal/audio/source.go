package audio

const (
	ipcMsgTypeOpus = 0
)

type AudioConfig struct {
	Bitrate        uint16
	Complexity     uint8
	BufferPeriods  uint8
	DTXEnabled     bool
	FECEnabled     bool
	SampleRate     uint32
	PacketLossPerc uint8
}

func DefaultAudioConfig() AudioConfig {
	return AudioConfig{
		Bitrate:        192,
		Complexity:     8,
		BufferPeriods:  12,
		DTXEnabled:     false,
		FECEnabled:     true,
		SampleRate:     48000,
		PacketLossPerc: 0,
	}
}

type AudioSource interface {
	ReadMessage() (msgType uint8, payload []byte, err error)
	WriteMessage(msgType uint8, payload []byte) error
	IsConnected() bool
	Connect() error
	Disconnect()
}
