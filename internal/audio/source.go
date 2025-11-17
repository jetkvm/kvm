package audio

const (
	ipcMsgTypeOpus = 0
)

type AudioConfig struct {
	Bitrate       uint16
	Complexity    uint8
	BufferPeriods uint8
	DTXEnabled    bool
	FECEnabled    bool
}

func DefaultAudioConfig() AudioConfig {
	return AudioConfig{
		Bitrate:       128,
		Complexity:    5,
		BufferPeriods: 12,
		DTXEnabled:    true,
		FECEnabled:    true,
	}
}

type AudioSource interface {
	ReadMessage() (msgType uint8, payload []byte, err error)
	WriteMessage(msgType uint8, payload []byte) error
	IsConnected() bool
	Connect() error
	Disconnect()
	SetConfig(cfg AudioConfig)
}
