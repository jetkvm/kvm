package kvm

import (
	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/native"
)

var nativeInstance *native.Native

func initNative(systemVersion *semver.Version, appVersion *semver.Version) {
	nativeInstance = native.NewNative(systemVersion, appVersion)
	nativeInstance.Start()
}
