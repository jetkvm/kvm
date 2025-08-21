package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	magicNumber uint32 = 0x4A4B564D // "JKVM"
	socketName         = "audio_output.sock"
)

type AudioServer struct {
	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
}

func NewAudioServer() (*AudioServer, error) {
	socketPath := filepath.Join("/var/run", socketName)
	// Remove existing socket if any
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create unix socket: %w", err)
	}

	return &AudioServer{listener: listener}, nil
}

func (s *AudioServer) Start() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept connection: %w", err)
	}
	s.conn = conn
	return nil
}

func (s *AudioServer) Close() error {
	if s.conn != nil {
		s.conn.Close()
	}
	return s.listener.Close()
}

func (s *AudioServer) SendFrame(frame []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no client connected")
	}

	// Write magic number
	if err := binary.Write(s.conn, binary.BigEndian, magicNumber); err != nil {
		return fmt.Errorf("failed to write magic number: %w", err)
	}

	// Write frame size
	if err := binary.Write(s.conn, binary.BigEndian, uint32(len(frame))); err != nil {
		return fmt.Errorf("failed to write frame size: %w", err)
	}

	// Write frame data
	if _, err := s.conn.Write(frame); err != nil {
		return fmt.Errorf("failed to write frame data: %w", err)
	}

	return nil
}

type AudioClient struct {
	conn net.Conn
	mtx  sync.Mutex
}

func NewAudioClient() (*AudioClient, error) {
	socketPath := filepath.Join("/var/run", socketName)
	// Try connecting multiple times as the server might not be ready
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			return &AudioClient{conn: conn}, nil
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("failed to connect to audio server")
}

func (c *AudioClient) Close() error {
	return c.conn.Close()
}

func (c *AudioClient) ReceiveFrame() ([]byte, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Read magic number
	var magic uint32
	if err := binary.Read(c.conn, binary.BigEndian, &magic); err != nil {
		return nil, fmt.Errorf("failed to read magic number: %w", err)
	}
	if magic != magicNumber {
		return nil, fmt.Errorf("invalid magic number: %x", magic)
	}

	// Read frame size
	var size uint32
	if err := binary.Read(c.conn, binary.BigEndian, &size); err != nil {
		return nil, fmt.Errorf("failed to read frame size: %w", err)
	}

	// Read frame data
	frame := make([]byte, size)
	if _, err := io.ReadFull(c.conn, frame); err != nil {
		return nil, fmt.Errorf("failed to read frame data: %w", err)
	}

	return frame, nil
}
