package resource

import (
	"embed"
)

//go:embed jetkvm_native jetkvm_native.sha256 netboot.xyz-multiarch.iso
var ResourceFS embed.FS
