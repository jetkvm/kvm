package native

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Request represents a JSON-RPC request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Event represents a JSON-RPC notification/event
type Event struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// ProcessConfig is the configuration for the native process
type ProcessConfig struct {
	Disable              bool    `json:"disable"`
	SystemVersion        string  `json:"system_version"` // Serialized as string
	AppVersion           string  `json:"app_version"`    // Serialized as string
	DisplayRotation      uint16  `json:"display_rotation"`
	DefaultQualityFactor float64 `json:"default_quality_factor"`
}

// IPCClient handles communication with the native process
type IPCClient struct {
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	logger *zerolog.Logger

	requestID  int64
	requestIDM sync.Mutex

	pendingRequests map[interface{}]chan *Response
	pendingM        sync.Mutex

	eventHandlers map[string][]func(data interface{})
	eventM        sync.RWMutex

	readyCh chan struct{}
	ready   bool

	closed bool
	closeM sync.Mutex
}

type processCmd interface {
	Start() error
	Wait() error
	GetProcess() interface {
		Kill() error
		Signal(sig interface{}) error
	}
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
}

func NewIPCClient(cmd processCmd, logger *zerolog.Logger) (*IPCClient, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	client := &IPCClient{
		stdin:           stdin,
		stdout:          bufio.NewScanner(stdout),
		logger:          logger,
		pendingRequests: make(map[interface{}]chan *Response),
		eventHandlers:   make(map[string][]func(data interface{})),
		readyCh:         make(chan struct{}),
	}

	// Start reading responses
	go client.readLoop()

	return client, nil
}

func (c *IPCClient) readLoop() {
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		// Try to parse as response
		var resp Response
		if err := json.Unmarshal(line, &resp); err == nil {
			// Check if it's a ready signal (result is "ready" and no ID)
			if resp.Result == "ready" && resp.ID == nil && !c.ready {
				c.ready = true
				close(c.readyCh)
				continue
			}
			if resp.Result != nil || resp.Error != nil {
				c.handleResponse(&resp)
				continue
			}
		}

		// Try to parse as event
		var event Event
		if err := json.Unmarshal(line, &event); err == nil && event.Method == "event" {
			c.handleEvent(&event)
			continue
		}

		c.logger.Warn().Bytes("line", line).Msg("unexpected message from native process")
	}

	c.closeM.Lock()
	if !c.closed {
		c.closed = true
		c.closeM.Unlock()
		c.logger.Warn().Msg("native process stdout closed")
		// Cancel all pending requests
		c.pendingM.Lock()
		for id, ch := range c.pendingRequests {
			close(ch)
			delete(c.pendingRequests, id)
		}
		c.pendingM.Unlock()
	} else {
		c.closeM.Unlock()
	}
}

func (c *IPCClient) handleResponse(resp *Response) {
	c.pendingM.Lock()
	ch, ok := c.pendingRequests[resp.ID]
	if ok {
		delete(c.pendingRequests, resp.ID)
	}
	c.pendingM.Unlock()

	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

func (c *IPCClient) handleEvent(event *Event) {
	if event.Method != "event" || event.Params == nil {
		return
	}

	eventType, ok := event.Params["type"].(string)
	if !ok {
		return
	}

	data := event.Params["data"]

	c.eventM.RLock()
	handlers := c.eventHandlers[eventType]
	c.eventM.RUnlock()

	for _, handler := range handlers {
		handler(data)
	}
}

func (c *IPCClient) Call(method string, params interface{}) (*Response, error) {
	c.closeM.Lock()
	if c.closed {
		c.closeM.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.closeM.Unlock()

	c.requestIDM.Lock()
	c.requestID++
	id := c.requestID
	c.requestIDM.Unlock()

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = paramsBytes
	}

	ch := make(chan *Response, 1)
	c.pendingM.Lock()
	c.pendingRequests[id] = ch
	c.pendingM.Unlock()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		c.pendingM.Lock()
		delete(c.pendingRequests, id)
		c.pendingM.Unlock()
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := c.stdin.Write(append(reqBytes, '\n')); err != nil {
		c.pendingM.Lock()
		delete(c.pendingRequests, id)
		c.pendingM.Unlock()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		c.pendingM.Lock()
		delete(c.pendingRequests, id)
		c.pendingM.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

func (c *IPCClient) OnEvent(eventType string, handler func(data interface{})) {
	c.eventM.Lock()
	defer c.eventM.Unlock()
	c.eventHandlers[eventType] = append(c.eventHandlers[eventType], handler)
}

func (c *IPCClient) WaitReady() error {
	select {
	case <-c.readyCh:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for ready signal")
	}
}

func (c *IPCClient) Close() error {
	c.closeM.Lock()
	defer c.closeM.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	c.pendingM.Lock()
	for id, ch := range c.pendingRequests {
		close(ch)
		delete(c.pendingRequests, id)
	}
	c.pendingM.Unlock()

	return c.stdin.Close()
}

