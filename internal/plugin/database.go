package plugin

import (
	"encoding/json"
	"fmt"
	"os"
)

const databaseFile = pluginsFolder + "/plugins.json"

var pluginDatabase = PluginDatabase{}

func init() {
	if err := pluginDatabase.Load(); err != nil {
		fmt.Printf("failed to load plugin database: %v\n", err)
	}
}

func (d *PluginDatabase) Load() error {
	file, err := os.Open(databaseFile)
	if os.IsNotExist(err) {
		d.Plugins = make(map[string]PluginInstall)
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

	file, err := os.Create(databaseFile)
	if err != nil {
		return fmt.Errorf("failed to create plugin database: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(d); err != nil {
		return fmt.Errorf("failed to encode plugin database: %v", err)
	}

	return nil
}
