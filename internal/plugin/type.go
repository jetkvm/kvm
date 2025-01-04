package plugin

type PluginManifest struct {
	ManifestVersion  string `json:"manifest_version"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Description      string `json:"description,omitempty"`
	Homepage         string `json:"homepage"`
	BinaryPath       string `json:"bin"`
	SystemMinVersion string `json:"system_min_version,omitempty"`
}

type PluginStatus struct {
	PluginManifest
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}
