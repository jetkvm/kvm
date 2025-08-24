package audio

import (
	"fmt"
	"net"
	"syscall"
)

const (
	// Socket buffer sizes optimized for JetKVM's audio workload
	OptimalSocketBuffer = 128 * 1024 // 128KB (32 frames @ 4KB each)
	MaxSocketBuffer     = 256 * 1024 // 256KB for high-load scenarios
	MinSocketBuffer     = 32 * 1024  // 32KB minimum for basic functionality
)

// SocketBufferConfig holds socket buffer configuration
type SocketBufferConfig struct {
	SendBufferSize int
	RecvBufferSize int
	Enabled        bool
}

// DefaultSocketBufferConfig returns the default socket buffer configuration
func DefaultSocketBufferConfig() SocketBufferConfig {
	return SocketBufferConfig{
		SendBufferSize: OptimalSocketBuffer,
		RecvBufferSize: OptimalSocketBuffer,
		Enabled:        true,
	}
}

// HighLoadSocketBufferConfig returns configuration for high-load scenarios
func HighLoadSocketBufferConfig() SocketBufferConfig {
	return SocketBufferConfig{
		SendBufferSize: MaxSocketBuffer,
		RecvBufferSize: MaxSocketBuffer,
		Enabled:        true,
	}
}

// ConfigureSocketBuffers applies socket buffer configuration to a Unix socket connection
func ConfigureSocketBuffers(conn net.Conn, config SocketBufferConfig) error {
	if !config.Enabled {
		return nil
	}

	if err := ValidateSocketBufferConfig(config); err != nil {
		return fmt.Errorf("invalid socket buffer config: %w", err)
	}

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("connection is not a Unix socket")
	}

	file, err := unixConn.File()
	if err != nil {
		return fmt.Errorf("failed to get socket file descriptor: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())

	if config.SendBufferSize > 0 {
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, config.SendBufferSize); err != nil {
			return fmt.Errorf("failed to set SO_SNDBUF to %d: %w", config.SendBufferSize, err)
		}
	}

	if config.RecvBufferSize > 0 {
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, config.RecvBufferSize); err != nil {
			return fmt.Errorf("failed to set SO_RCVBUF to %d: %w", config.RecvBufferSize, err)
		}
	}

	return nil
}

// GetSocketBufferSizes retrieves current socket buffer sizes
func GetSocketBufferSizes(conn net.Conn) (sendSize, recvSize int, err error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("socket buffer query only supported for Unix sockets")
	}

	file, err := unixConn.File()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get socket file descriptor: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())

	// Get send buffer size
	sendSize, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get SO_SNDBUF: %w", err)
	}

	// Get receive buffer size
	recvSize, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get SO_RCVBUF: %w", err)
	}

	return sendSize, recvSize, nil
}

// ValidateSocketBufferConfig validates socket buffer configuration
func ValidateSocketBufferConfig(config SocketBufferConfig) error {
	if !config.Enabled {
		return nil
	}

	if config.SendBufferSize < MinSocketBuffer {
		return fmt.Errorf("send buffer size %d is below minimum %d", config.SendBufferSize, MinSocketBuffer)
	}

	if config.RecvBufferSize < MinSocketBuffer {
		return fmt.Errorf("receive buffer size %d is below minimum %d", config.RecvBufferSize, MinSocketBuffer)
	}

	if config.SendBufferSize > MaxSocketBuffer {
		return fmt.Errorf("send buffer size %d exceeds maximum %d", config.SendBufferSize, MaxSocketBuffer)
	}

	if config.RecvBufferSize > MaxSocketBuffer {
		return fmt.Errorf("receive buffer size %d exceeds maximum %d", config.RecvBufferSize, MaxSocketBuffer)
	}

	return nil
}

// RecordSocketBufferMetrics records socket buffer metrics for monitoring
func RecordSocketBufferMetrics(conn net.Conn, component string) {
	if conn == nil {
		return
	}

	// Get current socket buffer sizes
	sendSize, recvSize, err := GetSocketBufferSizes(conn)
	if err != nil {
		// Log error but don't fail
		return
	}

	// Record buffer sizes
	socketBufferSizeGauge.WithLabelValues(component, "send").Set(float64(sendSize))
	socketBufferSizeGauge.WithLabelValues(component, "receive").Set(float64(recvSize))
}

// RecordSocketBufferOverflow records a socket buffer overflow event
func RecordSocketBufferOverflow(component, bufferType string) {
	socketBufferOverflowCounter.WithLabelValues(component, bufferType).Inc()
}

// UpdateSocketBufferUtilization updates socket buffer utilization metrics
func UpdateSocketBufferUtilization(component, bufferType string, utilizationPercent float64) {
	socketBufferUtilizationGauge.WithLabelValues(component, bufferType).Set(utilizationPercent)
}
