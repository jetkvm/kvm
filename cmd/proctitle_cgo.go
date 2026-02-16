//go:build cgo && linux

package main

import "github.com/erikdubbelboer/gspt"

func setProcessTitle(title string) {
	gspt.SetProcTitle(title)
}
