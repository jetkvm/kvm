//go:build cgo && linux

package kvm

import "github.com/erikdubbelboer/gspt"

func setProcessTitle(title string) {
	gspt.SetProcTitle(title)
}
