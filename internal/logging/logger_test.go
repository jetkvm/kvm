package logging

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLogOutputOwnsMessageAfterWrite(t *testing.T) {
	// Keep this test serial: logOutput uses the package-wide SSE server.
	originalServer := sseServer
	server := &sseEvent{Message: make(chan string)}
	sseServer = server
	t.Cleanup(func() { sseServer = originalServer })

	output := &logOutput{mu: &sync.Mutex{}}
	buffer := make([]byte, 0, 128)
	for i := 0; i < 64; i++ {
		want := fmt.Sprintf("{\"message\":\"entry %d\"}\n", i)
		buffer = append(buffer[:0], want...)
		n, err := output.Write(buffer)
		if err != nil || n != len(buffer) {
			t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(buffer))
		}

		// A writer's caller may immediately overwrite and reuse its buffer,
		// even while an asynchronous consumer is not ready to receive.
		clear(buffer)
		select {
		case got := <-server.Message:
			if got != want {
				t.Fatalf("message after buffer reuse = %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for log message")
		}
	}
}
