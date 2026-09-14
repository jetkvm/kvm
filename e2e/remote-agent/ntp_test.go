package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestNTPReply(t *testing.T) {
	request := make([]byte, 48)
	request[0] = 0x23
	copy(request[40:], "request1")
	reply := ntpReply(request, time.Unix(1800000000, 500000000))
	if len(reply) != 48 || reply[0]&7 != 4 || reply[1] != 2 || !bytes.Equal(reply[24:32], request[40:48]) {
		t.Fatalf("invalid reply: %x", reply)
	}
	want := uint64(4008988800)<<32 | 1<<31
	for _, offset := range []int{16, 32, 40} {
		if binary.BigEndian.Uint64(reply[offset:]) != want {
			t.Fatalf("wrong timestamp at %d", offset)
		}
	}
	for _, bad := range [][]byte{[]byte("short"), make([]byte, 48)} {
		if ntpReply(bad, time.Now()) != nil {
			t.Fatal("answered malformed/non-client request")
		}
	}
}

func listenTestNTP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestServeNTPFiltersAndStopsWithOwner(t *testing.T) {
	for _, source := range []string{"127.0.0.1", "192.0.2.1"} {
		t.Run(source, func(t *testing.T) {
			conn := listenTestNTP(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reports := make(chan struct{}, 2)
			done := make(chan error, 1)
			go func() {
				done <- serveNTP(ctx, conn, netip.MustParseAddr(source), func() error { reports <- struct{}{}; return nil })
			}()
			client, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.SetDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := client.Write([]byte("invalid")); err != nil {
				t.Fatal(err)
			}
			request := make([]byte, 48)
			request[0] = 0x23
			if _, err := client.Write(request); err != nil {
				t.Fatal(err)
			}
			n, err := client.Read(make([]byte, 48))
			if source == "127.0.0.1" && (err != nil || n != 48) {
				t.Fatalf("reply: %d, %v", n, err)
			}
			if source != "127.0.0.1" && err == nil {
				t.Fatal("answered another device")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("responder survived owner cancellation")
			}
			want := 0
			if source == "127.0.0.1" {
				want = 1
			}
			if len(reports) != want {
				t.Fatalf("got %d requests, want %d", len(reports), want)
			}
			reopened, err := net.ListenUDP("udp4", conn.LocalAddr().(*net.UDPAddr))
			if err != nil {
				t.Fatal("responder did not release port:", err)
			}
			reopened.Close()
		})
	}
}

func TestServeNTPExpires(t *testing.T) {
	conn := listenTestNTP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := serveNTP(ctx, conn, netip.MustParseAddr("127.0.0.1"), func() error { t.Fatal("unexpected packet"); return nil })
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expiry: %v", err)
	}
}

func TestNTPRejectsInvalidSource(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"source":"::1"}`, `{"source":"hostname"}`} {
		w := httptest.NewRecorder()
		handleNTP(w, httptest.NewRequest("POST", "/ntp", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid source: %s", w.Body)
		}
	}
}
