package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sync"
)

const databaseFile = pluginsFolder + "/plugins.json"

type PluginDatabase struct {
	// Map with the plugin name as the key
	Plugins map[string]*PluginInstall `json:"plugins"`

	saveMutex sync.Mutex
}

var pluginDatabase = PluginDatabase{}

func (d *PluginDatabase) Load() error {
	file, err := os.Open(databaseFile)
	if os.IsNotExist(err) {
		d.Plugins = make(map[string]*PluginInstall)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open plugin database: %v", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(d); err != nil {
		return fmt.Errorf("failed to decode plugin database: %v", err)
	}

	return nil
}

func (d *PluginDatabase) Save() error {
	d.saveMutex.Lock()
	defer d.saveMutex.Unlock()

	file, err := os.Create(databaseFile + ".tmp")
	if err != nil {
		return fmt.Errorf("failed to create plugin database tmp: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(d); err != nil {
		return fmt.Errorf("failed to encode plugin database: %v", err)
	}

	if err := os.Rename(databaseFile+".tmp", databaseFile); err != nil {
		return fmt.Errorf("failed to move plugin database to active file: %v", err)
	}

	return nil
}

// Find all extract directories that are not referenced in the Plugins map and remove them
func (d *PluginDatabase) CleanupExtractDirectories() error {
	extractDirectories, err := os.ReadDir(pluginsExtractsFolder)
	if err != nil {
		return fmt.Errorf("failed to read extract directories: %v", err)
	}

	for _, extractDir := range extractDirectories {
		found := false
		for _, pluginInstall := range d.Plugins {
			for _, extractedFolder := range pluginInstall.ExtractedVersions {
				if extractDir.Name() == extractedFolder {
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		if !found {
			if err := os.RemoveAll(path.Join(pluginsExtractsFolder, extractDir.Name())); err != nil {
				return fmt.Errorf("failed to remove extract directory: %v", err)
			}
		}
	}

	return nil
}
