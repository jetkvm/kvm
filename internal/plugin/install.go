package plugin

type PluginInstall struct {
	Enabled bool `json:"enabled"`

	// Current active version of the plugin
	Version string `json:"version"`

	// Map of a plugin version to the extracted directory
	ExtractedVersions map[string]string `json:"extracted_versions"`

	manifest *PluginManifest
}

func (p *PluginInstall) GetManifest() (*PluginManifest, error) {
	if p.manifest != nil {
		return p.manifest, nil
	}

	manifest, err := readManifest(p.GetExtractedFolder())
	if err != nil {
		return nil, err
	}

	p.manifest = manifest
	return manifest, nil
}

func (p *PluginInstall) GetExtractedFolder() string {
	return p.ExtractedVersions[p.Version]
}
