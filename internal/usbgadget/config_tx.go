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
	c                     *ChangeSet
	log                   *zerolog.Logger
	reorderSymlinkChanges *RequestedFileChange
}

func (u *UsbGadget) newUsbGadgetTransaction() *UsbGadgetTransaction {
	tx := &UsbGadgetTransaction{
		c:   &ChangeSet{},
		log: u.log,
	}
	return tx
}

func (u *UsbGadget) WithTransaction(fn func(u2 *UsbGadget, tx *UsbGadgetTransaction) error) error {
	u.txLock.Lock()
	defer u.txLock.Unlock()

	logger := u.log.With().Str("udc", u.udc).Logger()
	logger.Info().Msg("starting USB gadget transaction")

	tx := u.newUsbGadgetTransaction()
	if err := fn(u, tx); err != nil {
		logger.Error().Err(err).Msg("transaction failed")
		return err
	}

	err := tx.Commit()
	logger.Trace().Err(err).Msg("committed transaction")
	return err
}

func (u *UsbGadget) getOrderedConfigItems() orderedGadgetConfigItems {
	items := make([]gadgetConfigItemWithKey, 0)
	for key, item := range u.configMap {
		items = append(items, gadgetConfigItemWithKey{key, item})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].item.order < items[j].item.order
	})

	return items
}

func (tx *UsbGadgetTransaction) addFileChange(component string, change RequestedFileChange) string {
	change.Component = component
	tx.c.AddFileChangeStruct(change)

	logger := tx.log
	logger.Trace().Interface("change", change).Msg("add change")

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

	logger := tx.log
	err := tx.c.Apply(logger)
	if err != nil {
		logger.Error().Err(err).Msg("failed to update usbgadget configuration")
		return err
	}
	logger.Info().Msg("usbgadget configuration updated")
	return nil
}

func (tx *UsbGadgetTransaction) MountConfigFS() {
	tx.addFileChange("gadget", RequestedFileChange{
		Path:          configFSPath,
		ExpectedState: FileStateMountedConfigFS,
		Description:   "mount configfs",
	})
}

func (tx *UsbGadgetTransaction) CreateConfigPath(configC1Path string) {
	tx.mkdirAll(
		"gadget",
		configC1Path,
		"create config path",
		[]string{configFSPath},
	)
}

func (tx *UsbGadgetTransaction) WriteGadgetConfig(kvmGadgetPath string, configC1Path string, udc string, orderedConfigItems orderedGadgetConfigItems, enabledDevices *Devices) {
	// create kvm gadget path
	tx.mkdirAll(
		"gadget",
		kvmGadgetPath,
		"create kvm gadget path",
		[]string{configC1Path},
	)

	deps := make([]string, 0)
	deps = append(deps, kvmGadgetPath)

	for _, val := range orderedConfigItems {
		key := val.key
		item := val.item

		// check if the item is enabled in the config
		if !enabledDevices.isGadgetConfigItemEnabled(key) {
			tx.DisableGadgetItemConfig(configC1Path, item)
			continue
		}

		deps = tx.writeGadgetItemConfig(kvmGadgetPath, configC1Path, item, deps)
	}

	tx.WriteUDC(kvmGadgetPath, udc)
}

func (tx *UsbGadgetTransaction) DisableGadgetItemConfig(configC1Path string, item gadgetConfigItem) {
	// remove symlink if exists
	if item.configPath == nil {
		return
	}

	configPath := joinPath(configC1Path, item.configPath)
	_ = tx.removeFile("gadget", configPath, "remove symlink: disable gadget config")
}

func (tx *UsbGadgetTransaction) writeGadgetItemConfig(kvmGadgetPath string, configC1Path string, item gadgetConfigItem, deps []string) []string {
	component := item.device

	// create directory for the item
	files := make([]string, 0)
	files = append(files, deps...)

	gadgetItemPath := joinPath(kvmGadgetPath, item.path)
	if gadgetItemPath != kvmGadgetPath {
		gadgetItemDir := tx.mkdirAll(component, gadgetItemPath, "create gadget item directory", files)
		files = append(files, gadgetItemDir)
	}

	beforeChange := make([]string, 0)
	disableGadgetItemKey := fmt.Sprintf("disable-%s", item.device)

	if item.configPath != nil && item.configAttrs == nil {
		beforeChange = append(beforeChange, disableGadgetItemKey)
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
		configItemPath := joinPath(configC1Path, item.configPath)
		if configItemPath != configC1Path {
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
		configPath := joinPath(configC1Path, item.configPath)

		// the change will be only applied by `beforeChange`
		tx.addFileChange(component, RequestedFileChange{
			Key:           disableGadgetItemKey,
			Path:          configPath,
			ExpectedState: FileStateAbsent,
			When:          "beforeChange", // TODO: make it more flexible
			Description:   "remove symlink",
		})

		tx.addReorderSymlinkChange(configC1Path, configPath, gadgetItemPath, files)
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

func (tx *UsbGadgetTransaction) addReorderSymlinkChange(configC1Path string, path string, target string, deps []string) {
	logger := tx.log
	logger.Trace().Str("configC1Path", configC1Path).Str("path", path).Str("target", target).Msg("add reorder symlink change")

	if tx.reorderSymlinkChanges == nil {
		tx.reorderSymlinkChanges = &RequestedFileChange{
			Component:       "gadget-finalize",
			Key:             "reorder-symlinks",
			Path:            configC1Path,
			ExpectedState:   FileStateSymlinkInOrderConfigFS,
			ExpectedContent: []byte(target),
			Description:     "order symlinks",
			ParamSymlinks:   []symlink{},
		}
	}

	tx.reorderSymlinkChanges.DependsOn = append(tx.reorderSymlinkChanges.DependsOn, deps...)
	tx.reorderSymlinkChanges.ParamSymlinks = append(tx.reorderSymlinkChanges.ParamSymlinks, symlink{
		Path:   path,
		Target: target,
	})
}

func (tx *UsbGadgetTransaction) WriteUDC(kvmGadgetPath string, udc string) {
	// bound the gadget to a UDC (USB Device Controller)
	path := path.Join(kvmGadgetPath, "UDC")
	tx.addFileChange("udc", RequestedFileChange{
		Key:             "udc",
		Path:            path,
		ExpectedState:   FileStateFileContentMatch,
		ExpectedContent: []byte(udc),
		DependsOn:       []string{"reorder-symlinks"},
		Description:     "write UDC",
	})
}

func (tx *UsbGadgetTransaction) RebindUsb(udc string, ignoreUnbindError bool) {
	// remove the gadget from the UDC
	tx.addFileChange("udc", RequestedFileChange{
		Path:            path.Join(dwc3Path, "unbind"),
		ExpectedState:   FileStateFileWrite,
		ExpectedContent: []byte(udc),
		Description:     "unbind UDC",
		DependsOn:       []string{"udc"},
		IgnoreErrors:    ignoreUnbindError,
	})
	// bind the gadget to the UDC
	tx.addFileChange("udc", RequestedFileChange{
		Path:            path.Join(dwc3Path, "bind"),
		ExpectedState:   FileStateFileWrite,
		ExpectedContent: []byte(udc),
		Description:     "bind UDC",
		DependsOn:       []string{path.Join(dwc3Path, "unbind")},
	})
}
