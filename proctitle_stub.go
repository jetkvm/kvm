//go:build !cgo || !linux

package kvm

func setProcessTitle(title string) {}

