package keyboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

//go:embed layouts/*.kle.json
var layoutFS embed.FS

// layoutAliases maps alternative IDs to the actual KLE filename stem.
// Used when the canonical config ID doesn't match the file name.
var layoutAliases = map[string]string{
	"nl-BE": "fr_BE", // Belgian AZERTY — isoCode was "nl-BE" in the old system
}

// builtinLayouts is computed once at startup by scanning the embedded
// layouts/ directory. Each .kle.json file becomes a hyphenated ID
// (e.g. en_US.kle.json → "en-US"). Aliases are added on top.
var builtinLayouts = discoverBuiltinLayouts()

func discoverBuiltinLayouts() map[string]struct{} {
	layouts := make(map[string]struct{})

	entries, err := layoutFS.ReadDir("layouts")
	if err != nil {
		return layouts
	}

	const suffix = ".kle.json"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, suffix) {
			continue
		}
		// en_US.kle.json → "en-US"
		stem := strings.TrimSuffix(name, suffix)
		id := strings.ReplaceAll(stem, "_", "-")
		layouts[id] = struct{}{}
	}

	// Add aliases
	for alias := range layoutAliases {
		layouts[alias] = struct{}{}
	}

	return layouts
}

// The canonical layout IDs use hyphens (e.g. "en-US") to match existing device
// configs, while the KLE files on disk use underscores (en_US.kle.json).
// This function handles the conversion, plus any aliases.
func builtinLayoutFilename(id string) string {
	// Check for aliases first (e.g. nl-BE -> fr_BE)
	if alias, ok := layoutAliases[id]; ok {
		return path.Join("layouts", alias+".kle.json")
	}
	// Convert hyphens to underscores: "en-US" -> "en_US"
	fileStem := strings.ReplaceAll(id, "-", "_")
	return path.Join("layouts", fileStem+".kle.json")
}

func loadBuiltinLayoutFromFS(id string) (*KeyboardLayout, error) {
	filename := builtinLayoutFilename(id)
	data, err := layoutFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("built-in layout not found: %s", id)
	}
	return ParseKLE(data, id, "")
}

// loadBuiltinLayoutMetaFromFS reads only layout metadata needed for the
// settings list (ID + name), avoiding full ParseKLE processing.
func loadBuiltinLayoutMetaFromFS(id string) (LayoutMeta, error) {
	filename := builtinLayoutFilename(id)
	data, err := layoutFS.ReadFile(filename)
	if err != nil {
		return LayoutMeta{}, fmt.Errorf("built-in layout not found: %s", id)
	}

	var top []json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return LayoutMeta{}, fmt.Errorf("invalid built-in layout JSON: %s: %w", id, err)
	}

	name := id
	if len(top) > 0 {
		var meta kleMetadata
		if err := json.Unmarshal(top[0], &meta); err == nil {
			if meta.Name != "" {
				name = sanitizeName(meta.Name)
			}
		}
	}

	return LayoutMeta{ID: id, Name: name, Builtin: true}, nil
}
