//go:build linux && arm

package kvm

import "testing"

func TestRandomSignedOffsetStaysWithinBounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		j := randomSignedOffset(1, 3)
		if j == 0 {
			t.Fatalf("randomSignedOffset returned 0, want a non-zero nudge")
		}
		if j > 3 || j < -3 {
			t.Fatalf("randomSignedOffset(1, 3) = %d, want within [-3, 3]", j)
		}
	}
}
