package audio

// AudioSource provides audio frames from either CGO (in-process) or IPC (subprocess)
// This interface allows the relay goroutine to work with both modes transparently
type AudioSource interface {
	// ReadMessage reads the next audio message
	// Returns message type, payload data, and error
	// Blocks until data is available or error occurs
	ReadMessage() (msgType uint8, payload []byte, err error)

	// IsConnected returns true if the source is connected and ready
	IsConnected() bool

	// Connect establishes connection to the audio source
	// For CGO: initializes C audio subsystem
	// For IPC: connects to Unix socket
	Connect() error

	// Disconnect closes the connection and releases resources
	Disconnect()
}
