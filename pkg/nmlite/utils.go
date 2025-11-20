package nmlite

import (
	"sort"
	"time"

	"github.com/jetkvm/kvm/internal/network/types"
)

func lifetimeToTime(lifetime int) *time.Time {
	if lifetime == 0 {
		return nil
	}

	// Check for infinite lifetime (0xFFFFFFFF = 4294967295)
	// This is used for static/permanent addresses
	// Use uint32 to avoid int overflow on 32-bit systems
	const infiniteLifetime uint32 = 0xFFFFFFFF
	if uint32(lifetime) == infiniteLifetime || lifetime < 0 {
		return nil // Infinite lifetime - no expiration
	}

	// For finite lifetimes (SLAAC addresses)
	t := time.Now().Add(time.Duration(lifetime) * time.Second)
	return &t
}

func sortAndCompareStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	sort.Strings(a)
	sort.Strings(b)

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func sortIPv6AddressSlicesStable(a []types.IPv6Address) {
	sort.SliceStable(a, func(i, j int) bool {
		return a[i].Address.String() < a[j].Address.String()
	})
}

func sortAndCompareIPv6AddressSlices(a, b []types.IPv6Address) bool {
	if len(a) != len(b) {
		return false
	}

	sortIPv6AddressSlicesStable(a)
	sortIPv6AddressSlicesStable(b)

	for i := range a {
		if a[i].Address.String() != b[i].Address.String() {
			return false
		}

		if a[i].Prefix.String() != b[i].Prefix.String() {
			return false
		}

		if a[i].Flags != b[i].Flags {
			return false
		}

		// we don't compare the lifetimes because they are not always same
		if a[i].Scope != b[i].Scope {
			return false
		}
	}
	return true
}
