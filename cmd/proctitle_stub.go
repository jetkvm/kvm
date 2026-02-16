//go:build !cgo || !linux

package main

func setProcessTitle(title string) {}

