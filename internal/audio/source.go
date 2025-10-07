package audio

// AudioSource provides audio frames via CGO (in-process) C audio functions
type AudioSource interface {
	// ReadMessage reads the next audio message
	// Returns message type, payload data, and error
	// Blocks until data is available or error occurs
	// Used for output path (device → browser)
	ReadMessage() (msgType uint8, payload []byte, err error)

	// WriteMessage writes an audio message
	// Used for input path (browser → device)
	WriteMessage(msgType uint8, payload []byte) error

	// IsConnected returns true if the source is connected and ready
	IsConnected() bool

	// Connect initializes the C audio subsystem
	Connect() error

	// Disconnect closes the connection and releases resources
	Disconnect()
}
