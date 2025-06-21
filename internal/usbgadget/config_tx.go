package usbgadget

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"github.com/rs/zerolog"
)

// no os package should occur in this file

type UsbGadgetTransaction struct {
	c *ChangeSet

	// below are the fields that are needed to be set by the caller
	log                       *zerolog.Logger
	udc                       string
	dwc3Path                  string
	kvmGadgetPath             string
	configC1Path              string
	orderedConfigItems        orderedGadgetConfigItems
	isGadgetConfigItemEnabled func(key string) bool

	reorderSymlinkChanges *RequestedFileChange
}

func (u *UsbGadget) newUsbGadgetTransaction(lock bool) error {
	if lock {
		u.txLock.Lock()
		defer u.txLock.Unlock()
	}

	if u.tx != nil {
		return fmt.Errorf("transaction already exists")
	}

	tx := &UsbGadgetTransaction{
		c:                         &ChangeSet{},
		log:                       u.log,
		udc:                       u.udc,
		dwc3Path:                  dwc3Path,
		kvmGadgetPath:             u.kvmGadgetPath,
		configC1Path:              u.configC1Path,
		orderedConfigItems:        u.getOrderedConfigItems(),
		isGadgetConfigItemEnabled: u.isGadgetConfigItemEnabled,
	}
	u.tx = tx

	return nil
}

func (u *UsbGadget) WithTransaction(fn func() error) error {
	u.txLock.Lock()
	defer u.txLock.Unlock()

	err := u.newUsbGadgetTransaction(false)
	if err != nil {
		u.log.Error().Err(err).Msg("failed to create transaction")
		return err
	}
	if err := fn(); err != nil {
		u.log.Error().Err(err).Msg("transaction failed")
		return err
	}
	result := u.tx.Commit()
	u.tx = nil

	return result
}

func (tx *UsbGadgetTransaction) addFileChange(component string, change RequestedFileChange) string {
	change.Component = component
	tx.c.AddFileChangeStruct(change)

	key := change.Key
	if key == "" {
		key = change.Path
	}
	return key
}

func (tx *UsbGadgetTransaction) mkdirAll(component string, path string, description string, deps []string) string {
	return tx.addFileChange(component, RequestedFileChange{
		Path:          path,
		ExpectedState: FileStateDirectory,
		Description:   description,
		DependsOn:     deps,
	})
}

func (tx *UsbGadgetTransaction) removeFile(component string, path string, description string) string {
	return tx.addFileChange(component, RequestedFileChange{
		Path:          path,
		ExpectedState: FileStateAbsent,
		Description:   description,
	})
}

func (tx *UsbGadgetTransaction) Commit() error {
	tx.addFileChange("gadget-finalize", *tx.reorderSymlinkChanges)

	err := tx.c.Apply()
	if err != nil {
		tx.log.Error().Err(err).Msg("failed to update usbgadget configuration")
		return err
	}
	tx.log.Info().Msg("usbgadget configuration updated")
	return nil
}

func (u *UsbGadget) getOrderedConfigItems() orderedGadgetConfigItems {
	items := make([]gadgetConfigItemWithKey, 0)
	for key, item := range u.configMap {
		items = append(items, gadgetConfigItemWithKey{key, item})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].item.order < items[j].item.order
	})

	return items
}

func (tx *UsbGadgetTransaction) MountConfigFS() {
	tx.addFileChange("gadget", RequestedFileChange{
		Path:          configFSPath,
		ExpectedState: FileStateMountedConfigFS,
		Description:   "mount configfs",
	})
}

func (tx *UsbGadgetTransaction) CreateConfigPath() {
	tx.mkdirAll(
		"gadget",
		tx.configC1Path,
		"create config path",
		[]string{configFSPath},
	)
}

func (tx *UsbGadgetTransaction) WriteGadgetConfig() {
	// create kvm gadget path
	tx.mkdirAll(
		"gadget",
		tx.kvmGadgetPath,
		"create kvm gadget path",
		[]string{tx.configC1Path},
	)

	deps := make([]string, 0)
	deps = append(deps, tx.kvmGadgetPath)

	for _, val := range tx.orderedConfigItems {
		key := val.key
		item := val.item

		// check if the item is enabled in the config
		if !tx.isGadgetConfigItemEnabled(key) {
			tx.DisableGadgetItemConfig(item)
			continue
		}
		deps = tx.writeGadgetItemConfig(item, deps)
	}

	tx.WriteUDC()
}

func (tx *UsbGadgetTransaction) getDisableKeys() []string {
	disableKeys := make([]string, 0)
	for _, item := range tx.orderedConfigItems {
		if !tx.isGadgetConfigItemEnabled(item.key) {
			continue
		}
		if item.item.configPath == nil || item.item.configAttrs != nil {
			continue
		}

		disableKeys = append(disableKeys, fmt.Sprintf("disable-%s", item.item.device))
	}
	return disableKeys
}

func (tx *UsbGadgetTransaction) DisableGadgetItemConfig(item gadgetConfigItem) {
	// remove symlink if exists
	if item.configPath == nil {
		return
	}

	configPath := joinPath(tx.configC1Path, item.configPath)
	_ = tx.removeFile("gadget", configPath, "remove symlink: disable gadget config")
}

func (tx *UsbGadgetTransaction) writeGadgetItemConfig(item gadgetConfigItem, deps []string) []string {
	component := item.device

	// create directory for the item
	files := make([]string, 0)
	files = append(files, deps...)

	gadgetItemPath := joinPath(tx.kvmGadgetPath, item.path)
	if gadgetItemPath != tx.kvmGadgetPath {
		gadgetItemDir := tx.mkdirAll(component, gadgetItemPath, "create gadget item directory", files)
		files = append(files, gadgetItemDir)
	}

	beforeChange := make([]string, 0)
	disableGadgetItemKey := fmt.Sprintf("disable-%s", item.device)
	if item.configPath != nil && item.configAttrs == nil {
		beforeChange = append(beforeChange, tx.getDisableKeys()...)
	}

	if len(item.attrs) > 0 {
		// write attributes for the item
		files = append(files, tx.writeGadgetAttrs(
			gadgetItemPath,
			item.attrs,
			component,
			beforeChange,
		)...)
	}

	// write report descriptor if available
	reportDescPath := path.Join(gadgetItemPath, "report_desc")
	if item.reportDesc != nil {
		tx.addFileChange(component, RequestedFileChange{
			Path:            reportDescPath,
			ExpectedState:   FileStateFileContentMatch,
			ExpectedContent: item.reportDesc,
			Description:     "write report descriptor",
			BeforeChange:    beforeChange,
			DependsOn:       files,
		})
	} else {
		tx.addFileChange(component, RequestedFileChange{
			Path:          reportDescPath,
			ExpectedState: FileStateAbsent,
			Description:   "remove report descriptor",
			BeforeChange:  beforeChange,
			DependsOn:     files,
		})
	}
	files = append(files, reportDescPath)

	// create config directory if configAttrs are set
	if len(item.configAttrs) > 0 {
		configItemPath := joinPath(tx.configC1Path, item.configPath)
		if configItemPath != tx.configC1Path {
			configItemDir := tx.mkdirAll(component, configItemPath, "create config item directory", files)
			files = append(files, configItemDir)
		}
		files = append(files, tx.writeGadgetAttrs(
			configItemPath,
			item.configAttrs,
			component,
			beforeChange,
		)...)
	}

	// create symlink if configPath is set
	if item.configPath != nil && item.configAttrs == nil {
		configPath := joinPath(tx.configC1Path, item.configPath)

		// the change will be only applied by `beforeChange`
		tx.addFileChange(component, RequestedFileChange{
			Key:           disableGadgetItemKey,
			Path:          configPath,
			ExpectedState: FileStateAbsent,
			When:          "beforeChange", // TODO: make it more flexible
			Description:   "remove symlink",
		})

		tx.addReorderSymlinkChange(configPath, gadgetItemPath, files)
	}

	return files
}

func (tx *UsbGadgetTransaction) writeGadgetAttrs(basePath string, attrs gadgetAttributes, component string, beforeChange []string) (files []string) {
	files = make([]string, 0)
	for key, val := range attrs {
		filePath := filepath.Join(basePath, key)
		tx.addFileChange(component, RequestedFileChange{
			Path:            filePath,
			ExpectedState:   FileStateFileContentMatch,
			ExpectedContent: []byte(val),
			Description:     "write gadget attribute",
			DependsOn:       []string{basePath},
			BeforeChange:    beforeChange,
		})
		files = append(files, filePath)
	}
	return files
}

func (tx *UsbGadgetTransaction) addReorderSymlinkChange(path string, target string, deps []string) {
	tx.log.Trace().Str("path", path).Str("target", target).Msg("add reorder symlink change")

	if tx.reorderSymlinkChanges == nil {
		tx.reorderSymlinkChanges = &RequestedFileChange{
			Component:     "gadget-finalize",
			Key:           "reorder-symlinks",
			Path:          tx.configC1Path,
			ExpectedState: FileStateSymlinkInOrderConfigFS,
			Description:   "order symlinks",
			ParamSymlinks: []symlink{},
		}
	}

	tx.reorderSymlinkChanges.DependsOn = append(tx.reorderSymlinkChanges.DependsOn, deps...)
	tx.reorderSymlinkChanges.ParamSymlinks = append(tx.reorderSymlinkChanges.ParamSymlinks, symlink{
		Path:   path,
		Target: target,
	})
}

func (tx *UsbGadgetTransaction) WriteUDC() {
	// bound the gadget to a UDC (USB Device Controller)
	path := path.Join(tx.kvmGadgetPath, "UDC")
	tx.addFileChange("udc", RequestedFileChange{
		Key:             "udc",
		Path:            path,
		ExpectedState:   FileStateFileContentMatch,
		ExpectedContent: []byte(tx.udc),
		DependsOn:       []string{"reorder-symlinks"},
		Description:     "write UDC",
	})
}

func (tx *UsbGadgetTransaction) RebindUsb(ignoreUnbindError bool) {
	// remove the gadget from the UDC
	tx.addFileChange("udc", RequestedFileChange{
		Path:            path.Join(tx.dwc3Path, "unbind"),
		ExpectedState:   FileStateFileWrite,
		ExpectedContent: []byte(tx.udc),
		Description:     "unbind UDC",
		DependsOn:       []string{"udc"},
		IgnoreErrors:    ignoreUnbindError,
	})
	// bind the gadget to the UDC
	tx.addFileChange("udc", RequestedFileChange{
		Path:            path.Join(tx.dwc3Path, "bind"),
		ExpectedState:   FileStateFileWrite,
		ExpectedContent: []byte(tx.udc),
		Description:     "bind UDC",
		DependsOn:       []string{path.Join(tx.dwc3Path, "unbind")},
	})
}
