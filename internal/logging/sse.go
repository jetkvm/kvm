package logging

import (
	"embed"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

//go:embed sse.html
var sseHTML embed.FS

type sseEvent struct {
	Message       chan string
	NewClients    chan chan string
	ClosedClients chan chan string
	TotalClients  map[chan string]bool
}

// New event messages are broadcast to all registered client connection channels
type sseClientChan chan string

var (
	sseServer *sseEvent
	sseLogger *zerolog.Logger
)

func init() {
	sseServer = newSseServer()
	sseLogger = GetSubsystemLogger("sse")
}

// Initialize event and Start procnteessing requests
func newSseServer() (event *sseEvent) {
	event = &sseEvent{
		Message:       make(chan string),
		NewClients:    make(chan chan string),
		ClosedClients: make(chan chan string),
		TotalClients:  make(map[chan string]bool),
	}

	go event.listen()

	return
}

// It Listens all incoming requests from clients.
// Handles addition and removal of clients and broadcast messages to clients.
func (stream *sseEvent) listen() {
	for {
		select {
		// Add new available client
		case client := <-stream.NewClients:
			stream.TotalClients[client] = true
			sseLogger.Info().
				Int("total_clients", len(stream.TotalClients)).
				Msg("new client connected")

		// Remove closed client
		case client := <-stream.ClosedClients:
			delete(stream.TotalClients, client)
			close(client)
			sseLogger.Info().Int("total_clients", len(stream.TotalClients)).Msg("client disconnected")

		// Broadcast message to client
		case eventMsg := <-stream.Message:
			for clientMessageChan := range stream.TotalClients {
				select {
				case clientMessageChan <- eventMsg:
					// Message sent successfully
				default:
					// Failed to send, dropping message
				}
			}
		}
	}
}

func (stream *sseEvent) serveHTTP() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientChan := make(sseClientChan)
		stream.NewClients <- clientChan

		go func() {
			<-c.Writer.CloseNotify()

			for range clientChan {
			}

			stream.ClosedClients <- clientChan
		}()

		c.Set("clientChan", clientChan)
		c.Next()
	}
}

func sseHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" && c.NegotiateFormat(gin.MIMEHTML) == gin.MIMEHTML {
			c.FileFromFS("/sse.html", http.FS(sseHTML))
			c.Status(http.StatusOK)
			c.Abort()
			return
		}

		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Next()
	}
}

func AttachSSEHandler(router *gin.RouterGroup) {
	router.StaticFS("/log-stream", http.FS(sseHTML))
	router.GET("/log-stream", sseHeadersMiddleware(), sseServer.serveHTTP(), func(c *gin.Context) {
		v, ok := c.Get("clientChan")
		if !ok {
			return
		}
		clientChan, ok := v.(sseClientChan)
		if !ok {
			return
		}
		c.Stream(func(w io.Writer) bool {
			if msg, ok := <-clientChan; ok {
				c.SSEvent("message", msg)
				return true
			}
			return false
		})
	})
}
