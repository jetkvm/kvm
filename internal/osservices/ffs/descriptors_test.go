package ffs

import (
	"encoding/binary"
	"testing"
)

type testEndpoint struct {
	address    byte
	attributes byte
	packetSize uint16
}

// speedOf reads count descriptors from b: one interface and its endpoints.
// It returns the interface's bNumEndpoints, the endpoints and the rest of b.
func speedOf(t *testing.T, b []byte, count uint32) (byte, []testEndpoint, []byte) {
	t.Helper()
	var declared byte
	var endpoints []testEndpoint
	for i := uint32(0); i < count; i++ {
		if len(b) < 2 || int(b[0]) > len(b) {
			t.Fatalf("descriptor %d is cut off", i)
		}
		switch {
		case i == 0 && b[1] == 4 && b[0] == 9:
			declared = b[4]
		case i > 0 && b[1] == 5 && b[0] == 7:
			endpoints = append(endpoints, testEndpoint{b[2], b[3], binary.LittleEndian.Uint16(b[4:])})
		default:
			t.Fatalf("descriptor %d has type %d and length %d", i, b[1], b[0])
		}
		b = b[b[0]:]
	}
	return declared, endpoints, b
}

// The kernel takes the blob only when each speed has as many descriptors as
// the header counts and the interface counts its endpoints. Both speeds have
// the same endpoint addresses, as FunctionFS has one file per endpoint. A
// sender uses the last bulk OUT and the last bulk IN endpoint: these are
// endpoint 1 and the highest endpoint, which the KVM opens.
func TestDescriptorsEndpoints(t *testing.T) {
	for _, spareIN := range []bool{false, true} {
		b := Descriptors(spareIN)
		if got := binary.LittleEndian.Uint32(b[4:]); int(got) != len(b) {
			t.Fatalf("spare %t: header length %d, blob %d", spareIN, got, len(b))
		}
		fsCount := binary.LittleEndian.Uint32(b[12:])
		hsCount := binary.LittleEndian.Uint32(b[16:])
		fsDeclared, fs, rest := speedOf(t, b[24:], fsCount)
		hsDeclared, hs, rest := speedOf(t, rest, hsCount)
		// The Microsoft OS descriptors follow. The first is the extended
		// compat ID: interface 0, and a length of the 11-byte header and one
		// 24-byte entry.
		if len(rest) < 5 || rest[0] != 0 || binary.LittleEndian.Uint32(rest[1:]) != 11+24 {
			t.Fatalf("spare %t: the OS descriptors do not follow the endpoint descriptors", spareIN)
		}

		want := 2
		if spareIN {
			want = 3
		}
		if len(fs) != want || int(fsDeclared) != want || len(hs) != want || int(hsDeclared) != want {
			t.Fatalf("spare %t: endpoints fs %d (interface says %d), hs %d (interface says %d), want %d",
				spareIN, len(fs), fsDeclared, len(hs), hsDeclared, want)
		}
		for i := range fs {
			if fs[i].address != hs[i].address || fs[i].attributes != hs[i].attributes {
				t.Fatalf("spare %t: endpoint %d differs between the speeds", spareIN, i)
			}
			// FunctionFS names the files ep1, ep2, ... in this order.
			if number := int(fs[i].address & 0x0f); number != i+1 {
				t.Fatalf("spare %t: endpoint %d has number %d", spareIN, i, number)
			}
		}

		out, in := fs[0], fs[len(fs)-1]
		if out.address&0x80 != 0 || out.attributes != transferBulk {
			t.Fatalf("spare %t: endpoint 1 is not bulk OUT", spareIN)
		}
		if in.address&0x80 == 0 || in.attributes != transferBulk {
			t.Fatalf("spare %t: the last endpoint is not bulk IN", spareIN)
		}
		if fs[0].packetSize != PacketSizeFS || hs[0].packetSize != PacketSizeHS ||
			fs[len(fs)-1].packetSize != PacketSizeFS || hs[len(hs)-1].packetSize != PacketSizeHS {
			t.Fatalf("spare %t: bulk packet sizes are not %d and %d", spareIN, PacketSizeFS, PacketSizeHS)
		}
		if spareIN {
			spare := fs[1]
			if spare.address&0x80 == 0 || spare.attributes == transferBulk {
				t.Fatal("the spare endpoint must be an IN endpoint that a sender does not take: not bulk")
			}
		}
	}
}
