package kvm

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jetkvm/kvm/internal/osservices/installer"
)

// The OS Services installer disc: a small CD-ROM whose one page links to
// the OS Services download for this firmware version. The firmware makes it
// from its version and keeps it outside the images folder, so it is not
// listed with the user's images.
const builtinInstallerFolder = "/userdata/jetkvm/os_services"

var builtinInstallerPath = filepath.Join(builtinInstallerFolder, installer.Filename)

// writeBuiltinInstaller writes the disc for this firmware, unless the file
// already holds it.
func writeBuiltinInstaller(image []byte) error {
	if current, err := os.ReadFile(builtinInstallerPath); err == nil && bytes.Equal(current, image) {
		return nil
	}
	if err := os.MkdirAll(builtinInstallerFolder, 0o755); err != nil {
		return fmt.Errorf("failed to create the installer folder: %w", err)
	}
	tmp := builtinInstallerPath + ".tmp"
	if err := os.WriteFile(tmp, image, 0o644); err != nil {
		return fmt.Errorf("failed to write the installer: %w", err)
	}
	if err := os.Rename(tmp, builtinInstallerPath); err != nil {
		return fmt.Errorf("failed to write the installer: %w", err)
	}
	return nil
}

func prepareBuiltinInstallerMount() error {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState != nil {
		return errors.New("another virtual media is already mounted")
	}
	image := installer.Image(GetBuiltAppVersion())
	if err := writeBuiltinInstaller(image); err != nil {
		return err
	}
	// setMassStorageMode notes the KVM's own USB restart for OS Services
	// before it rebinds the gadget. A CD-ROM LUN is read-only.
	if err := setMassStorageMode(true); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}
	if err := gadget.SetMassStorageImage(builtinInstallerPath); err != nil {
		return fmt.Errorf("failed to set mass storage image: %w", err)
	}
	currentVirtualMediaState = &VirtualMediaState{
		Source:   Builtin,
		Mode:     CDROM,
		Filename: installer.Filename,
		Size:     int64(len(image)),
	}
	return nil
}

// rpcMountBuiltinInstaller mounts the installer disc as a CD-ROM. A mounted
// image stays: the browser unmounts it first.
func rpcMountBuiltinInstaller() error {
	if err := prepareBuiltinInstallerMount(); err != nil {
		return err
	}
	logger.Info().Str("path", builtinInstallerPath).Msg("OS Services installer mounted")
	if mqttManager != nil {
		mqttManager.publishVirtualMediaState()
	}
	return nil
}
