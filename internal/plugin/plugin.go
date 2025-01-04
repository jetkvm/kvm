package plugin

import (
	"encoding/json"
	"fmt"
	"kvm/internal/storage"
	"os"
	"path"

	"github.com/google/uuid"
)

const pluginsFolder = "/userdata/jetkvm/plugins"
const pluginsUploadFolder = pluginsFolder + "/uploads"

func init() {
	_ = os.MkdirAll(pluginsUploadFolder, 0755)
}

func RpcPluginStartUpload(filename string, size int64) (*storage.StorageFileUpload, error) {
	sanitizedFilename, err := storage.SanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(pluginsUploadFolder, sanitizedFilename)
	uploadPath := filePath + ".incomplete"

	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	var alreadyUploadedBytes int64 = 0
	if stat, err := os.Stat(uploadPath); err == nil {
		alreadyUploadedBytes = stat.Size()
	}

	uploadId := "plugin_" + uuid.New().String()
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for upload: %v", err)
	}

	storage.AddPendingUpload(uploadId, storage.PendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
	})

	return &storage.StorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}

func RpcPluginExtract(filename string) (*PluginManifest, error) {
	sanitizedFilename, err := storage.SanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(pluginsUploadFolder, sanitizedFilename)
	extractFolder, err := extractPlugin(filePath)
	if err != nil {
		return nil, err
	}

	if err := os.Remove(filePath); err != nil {
		return nil, fmt.Errorf("failed to delete uploaded file: %v", err)
	}

	manifest, err := readManifest(*extractFolder)
	if err != nil {
		return nil, err
	}

	// Get existing PluginInstall
	install, ok := pluginDatabase.Plugins[manifest.Name]
	if !ok {
		install = PluginInstall{
			Enabled:           false,
			Version:           manifest.Version,
			ExtractedVersions: make(map[string]string),
		}
	}

	_, ok = install.ExtractedVersions[manifest.Version]
	if ok {
		return nil, fmt.Errorf("this version has already been uploaded: %s", manifest.Version)
	}

	install.ExtractedVersions[manifest.Version] = *extractFolder
	pluginDatabase.Plugins[manifest.Name] = install

	if err := pluginDatabase.Save(); err != nil {
		return nil, fmt.Errorf("failed to save plugin database: %v", err)
	}

	return manifest, nil
}

func RpcPluginInstall(name string, version string) error {
	// TODO: find the plugin version in the plugins.json file
	pluginInstall, ok := pluginDatabase.Plugins[name]
	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if pluginInstall.Version == version && pluginInstall.Enabled {
		fmt.Printf("Plugin %s is already installed with version %s\n", name, version)
		return nil
	}

	_, ok = pluginInstall.ExtractedVersions[version]
	if !ok {
		return fmt.Errorf("plugin version not found: %s", version)
	}

	// TODO: If there is a running plugin with the same name, stop it and start the new version

	pluginInstall.Version = version
	pluginInstall.Enabled = true
	pluginDatabase.Plugins[name] = pluginInstall

	if err := pluginDatabase.Save(); err != nil {
		return fmt.Errorf("failed to save plugin database: %v", err)
	}
	// TODO: start the plugin

	// TODO: Determine if the old version should be removed

	return nil
}

func RpcPluginList() ([]PluginStatus, error) {
	plugins := make([]PluginStatus, 0, len(pluginDatabase.Plugins))
	for pluginName, plugin := range pluginDatabase.Plugins {
		status, err := plugin.GetStatus()
		if err != nil {
			return nil, fmt.Errorf("failed to get plugin status for %s: %v", pluginName, err)
		}
		plugins = append(plugins, *status)
	}
	return plugins, nil
}

func RpcUpdateConfig(name string, enabled bool) (*PluginStatus, error) {
	pluginInstall, ok := pluginDatabase.Plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}

	pluginInstall.Enabled = enabled
	pluginDatabase.Plugins[name] = pluginInstall

	if err := pluginDatabase.Save(); err != nil {
		return nil, fmt.Errorf("failed to save plugin database: %v", err)
	}

	status, err := pluginInstall.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin status for %s: %v", name, err)
	}
	return status, nil
}

func readManifest(extractFolder string) (*PluginManifest, error) {
	manifestPath := path.Join(extractFolder, "manifest.json")
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest file: %v", err)
	}
	defer manifestFile.Close()

	manifest := PluginManifest{}
	if err := json.NewDecoder(manifestFile).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %v", err)
	}

	if err := validateManifest(&manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest file: %v", err)
	}

	return &manifest, nil
}

func validateManifest(manifest *PluginManifest) error {
	if manifest.ManifestVersion != "1" {
		return fmt.Errorf("unsupported manifest version: %s", manifest.ManifestVersion)
	}

	if manifest.Name == "" {
		return fmt.Errorf("missing plugin name")
	}

	if manifest.Version == "" {
		return fmt.Errorf("missing plugin version")
	}

	if manifest.Homepage == "" {
		return fmt.Errorf("missing plugin homepage")
	}

	return nil
}
