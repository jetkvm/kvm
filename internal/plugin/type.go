package plugin

import "sync"

type PluginManifest struct {
	ManifestVersion  string `json:"manifest_version"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Description      string `json:"description,omitempty"`
	Homepage         string `json:"homepage"`
	BinaryPath       string `json:"bin"`
	SystemMinVersion string `json:"system_min_version,omitempty"`
}

type PluginInstall struct {
	Enabled bool `json:"enabled"`

	// Current active version of the plugin
	Version string `json:"version"`

	// Map of a plugin version to the extracted directory
	ExtractedVersions map[string]string `json:"extracted_versions"`
}

type PluginDatabase struct {
	// Map with the plugin name as the key
	Plugins map[string]PluginInstall `json:"plugins"`

	saveMutex sync.Mutex
}
