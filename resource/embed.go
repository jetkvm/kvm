package resource

import (
	"embed"
)

//go:embed netboot.xyz-multiarch.iso
var ResourceFS embed.FS
