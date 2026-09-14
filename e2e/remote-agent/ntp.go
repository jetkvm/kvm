package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"time"
)

func ntpReply(request []byte, now time.Time) []byte {
	if len(request) < 48 || request[0]&7 != 3 {
		return nil
	}
	packet := make([]byte, 48)
	copy(packet, []byte{0x24, 2, 6, 0xec})
	copy(packet[12:16], "TEST")
	stamp := uint64(uint32(now.Unix()+2208988800))<<32 | uint64(now.Nanosecond())*(1<<32)/1e9
	for _, offset := range []int{16, 32, 40} {
		binary.BigEndian.PutUint64(packet[offset:], stamp)
	}
	copy(packet[24:32], request[40:48])
	return packet
}

// The HTTP request owns the socket: cancellation or expiry stops the responder.
func serveNTP(ctx context.Context, conn *net.UDPConn, source netip.Addr, report func() error) error {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	packet := make([]byte, 512)
	for {
		n, peer, err := conn.ReadFromUDPAddrPort(packet)
		if err != nil {
			return err
		}
		if peer.Addr() != source {
			continue
		}
		reply := ntpReply(packet[:n], time.Now())
		if reply == nil {
			continue
		}
		if _, err := conn.WriteToUDPAddrPort(reply, peer); err != nil {
			return err
		}
		if err := report(); err != nil {
			return err
		}
	}
}

func handleNTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	source, parseErr := netip.ParseAddr(body.Source)
	if err != nil || parseErr != nil || !source.Is4() {
		http.Error(w, "expected the device's IPv4 address", http.StatusBadRequest)
		return
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 123})
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson")
	count := 0
	report := func() error {
		if err := json.NewEncoder(w).Encode(map[string]int{"requests": count}); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	if err := report(); err != nil {
		return
	}
	// A closed response tells the client the responder failed or expired.
	serveNTP(ctx, conn, source, func() error { count++; return report() })
}
