package keyboard

// HTTP handler for KLE layout upload and JSON-RPC handlers for layout management.
//
// Wire these into web.go / jsonrpc.go:
//
//   JSON-RPC (add to rpcHandlers map in jsonrpc.go):
//     "getKeyboardLayouts":    {Func: keyboard.RpcGetKeyboardLayouts},
//     "getKeyboardLayoutData": {Func: keyboard.RpcGetKeyboardLayoutData, Params: []string{"id"}},
//     "deleteKeyboardLayout":  {Func: keyboard.RpcDeleteKeyboardLayout, Params: []string{"id"}},

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	layoutsDir     = "/userdata/kvm_layouts"
	maxUploadBytes = 512 * 1024 // 512 KB
)

// HandleKeyboardUpload parses an uploaded KLE JSON file and stores the
// resulting KeyboardLayout.
//
// Body: raw KLE JSON (Content-Type: application/json)
// Query params:
//
//	?name=My+Layout    optional display name override
//	?id=existing-id    optional: replace an existing user-uploaded layout
//	                   (cannot replace built-in layouts)
//
// Response 200: LayoutUploadResponse JSON
// Response 400: { "error": "..." }
func HandleKeyboardUpload(c *gin.Context) {
	// Read body with size limit
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("failed to read body: %v", err)})
		return
	}
	if len(body) > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 512 KB)"})
		return
	}

	nameOverride := c.Query("name")
	idOverride := c.Query("id")

	// Block replacing built-in layouts
	if idOverride != "" {
		if _, ok := builtinLayouts[idOverride]; ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot replace built-in layout"})
			return
		}
	}

	layout, err := ParseKLE(body, idOverride, nameOverride)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	warnings := collectLayoutWarnings(layout)

	if err := storeLayout(layout); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to store layout: %v", err)})
		return
	}

	c.JSON(http.StatusOK, LayoutUploadResponse{
		ID:       layout.ID,
		Name:     layout.Name,
		KeyCount: len(layout.Keys),
		Warnings: warnings,
	})
}

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

func layoutPath(id string) string {
	// Sanitise ID — only allow alphanumeric, hyphens, underscores
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	return filepath.Join(layoutsDir, safe+".layout.json")
}

func storeLayout(layout *KeyboardLayout) error {
	if err := os.MkdirAll(layoutsDir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	return os.WriteFile(layoutPath(layout.ID), data, 0644)
}

func loadLayout(id string) (*KeyboardLayout, error) {
	// Try user-uploaded first, then built-in
	path := layoutPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		// Try built-in (embedded in binary via go:embed in a separate file)
		return loadBuiltinLayout(id)
	}
	var layout KeyboardLayout
	if err := json.Unmarshal(data, &layout); err != nil {
		return nil, fmt.Errorf("corrupt layout file %s: %w", id, err)
	}
	return &layout, nil
}

// loadBuiltinLayout reads a built-in layout from the embedded KLE files.
// Implemented in builtin.go via go:embed.
var loadBuiltinLayout = loadBuiltinLayoutFromFS

// ---------------------------------------------------------------------------
// JSON-RPC handlers
// ---------------------------------------------------------------------------

// RpcGetKeyboardLayouts returns the list of available layouts.
// Maps to JSON-RPC method "getKeyboardLayouts".
func RpcGetKeyboardLayouts() ([]LayoutMeta, error) {
	var layouts []LayoutMeta

	// Built-ins first
	for id := range builtinLayouts {
		l, err := loadLayout(id)
		if err != nil {
			continue // built-in not yet embedded — skip gracefully
		}
		layouts = append(layouts, LayoutMeta{ID: id, Name: l.Name, Builtin: true})
	}

	// Sort built-ins by name
	slices.SortFunc(layouts, func(a, b LayoutMeta) int {
		return strings.Compare(a.Name, b.Name)
	})

	// User-uploaded (appended after sorted built-ins)
	entries, err := os.ReadDir(layoutsDir)
	if err != nil && !os.IsNotExist(err) {
		return layouts, nil // return built-ins even if dir missing
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".layout.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".layout.json")
		if _, ok := builtinLayouts[id]; ok {
			continue // already included above
		}
		l, err := loadLayout(id)
		if err != nil {
			continue
		}
		layouts = append(layouts, LayoutMeta{ID: id, Name: l.Name, Builtin: false})
	}

	return layouts, nil
}

const fallbackLayoutID = "en-US"

// RpcGetKeyboardLayoutData returns the full KeyboardLayout for a given ID.
// If the requested layout is not found (deleted, corrupted, or invalid config),
// it falls back to the en-US built-in layout to ensure the UI always has a
// usable keyboard.
// Maps to JSON-RPC method "getKeyboardLayoutData".
func RpcGetKeyboardLayoutData(id string) (*KeyboardLayout, error) {
	if id == "" {
		id = fallbackLayoutID
	}
	layout, err := loadLayout(id)
	if err != nil && id != fallbackLayoutID {
		// Requested layout not found — fall back to en-US
		return loadLayout(fallbackLayoutID)
	}
	return layout, err
}

// RpcDeleteKeyboardLayout deletes a user-uploaded layout.
// Returns an error if the id is a built-in layout.
// Maps to JSON-RPC method "deleteKeyboardLayout".
func RpcDeleteKeyboardLayout(id string) error {
	if id == "" {
		return fmt.Errorf("id parameter required")
	}
	if _, ok := builtinLayouts[id]; ok {
		return fmt.Errorf("cannot delete built-in layout: %s", id)
	}
	path := layoutPath(id)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("layout not found: %s", id)
		}
		return fmt.Errorf("failed to delete layout: %w", err)
	}
	return nil
}
