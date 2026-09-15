package kvm

import (
	"fmt"
	"math"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// AbsMouseMappingConfig maps absolute mouse coordinates onto a sub-rectangle
// of the target's virtual desktop. The USB HID absolute pointer (0..32767) is
// interpreted by the target OS across the bounding box of the *entire*
// multi-monitor desktop, while JetKVM only captures a single screen. When
// enabled, coordinates are remapped so that the video area corresponds to the
// captured screen's rectangle within the full desktop.
// All dimensions share one unit (use the target OS's logical pixels).
type AbsMouseMappingConfig struct {
	Enabled      bool `json:"enabled"`
	TotalWidth   int  `json:"total_width"`
	TotalHeight  int  `json:"total_height"`
	ScreenX      int  `json:"screen_x"`
	ScreenY      int  `json:"screen_y"`
	ScreenWidth  int  `json:"screen_width"`
	ScreenHeight int  `json:"screen_height"`
}

// maxMappingDimension bounds all mapping values: far beyond any real desktop,
// small enough that int64 sums cannot overflow and float64 keeps precision.
const maxMappingDimension = 1 << 20

func (m *AbsMouseMappingConfig) validate() error {
	if !m.Enabled {
		return nil
	}
	if m.TotalWidth <= 0 || m.TotalHeight <= 0 {
		return fmt.Errorf("total desktop size must be positive")
	}
	if m.ScreenWidth <= 0 || m.ScreenHeight <= 0 {
		return fmt.Errorf("screen size must be positive")
	}
	if m.ScreenX < 0 || m.ScreenY < 0 {
		return fmt.Errorf("screen position must not be negative")
	}
	if m.TotalWidth > maxMappingDimension || m.TotalHeight > maxMappingDimension ||
		m.ScreenX > maxMappingDimension || m.ScreenY > maxMappingDimension ||
		m.ScreenWidth > maxMappingDimension || m.ScreenHeight > maxMappingDimension {
		return fmt.Errorf("mapping values must not exceed %d", maxMappingDimension)
	}
	// int64 arithmetic so absurd values can't wrap 32-bit int on arm builds
	if int64(m.ScreenX)+int64(m.ScreenWidth) > int64(m.TotalWidth) ||
		int64(m.ScreenY)+int64(m.ScreenHeight) > int64(m.TotalHeight) {
		return fmt.Errorf("screen rectangle must fit within the total desktop")
	}
	return nil
}

func clampAbsCoord(v float64) int {
	i := int(math.Round(v))
	if i < 0 {
		return 0
	}
	if i > 32767 {
		return 32767
	}
	return i
}

// absMouseMappingLock guards the config.AbsMouseMapping pointer. The setter
// always installs a freshly-validated struct and never mutates in place, so
// readers only need a consistent pointer snapshot.
var absMouseMappingLock sync.RWMutex

// absMouseMappingUpdateMu serializes whole set operations (swap + save +
// rollback) so a failing update cannot roll back over a newer successful one.
var absMouseMappingUpdateMu sync.Mutex

func currentAbsMouseMapping() *AbsMouseMappingConfig {
	absMouseMappingLock.RLock()
	defer absMouseMappingLock.RUnlock()
	if config == nil {
		return nil
	}
	return config.AbsMouseMapping
}

// applyAbsMouseMapping remaps x/y (0..32767 relative to the captured screen)
// into 0..32767 relative to the target's full desktop bounding box.
func applyAbsMouseMapping(x int, y int) (int, int) {
	m := currentAbsMouseMapping()
	if m == nil || !m.Enabled {
		return x, y
	}
	if m.validate() != nil {
		return x, y
	}
	nx := (float64(m.ScreenX) + float64(x)/32767.0*float64(m.ScreenWidth)) / float64(m.TotalWidth) * 32767.0
	ny := (float64(m.ScreenY) + float64(y)/32767.0*float64(m.ScreenHeight)) / float64(m.TotalHeight) * 32767.0
	return clampAbsCoord(nx), clampAbsCoord(ny)
}

func rpcGetAbsMouseMapping() (AbsMouseMappingConfig, error) {
	m := currentAbsMouseMapping()
	if m == nil {
		return AbsMouseMappingConfig{}, nil
	}
	return *m, nil
}

func rpcSetAbsMouseMapping(mapping AbsMouseMappingConfig) error {
	if err := mapping.validate(); err != nil {
		return err
	}
	absMouseMappingUpdateMu.Lock()
	defer absMouseMappingUpdateMu.Unlock()
	absMouseMappingLock.Lock()
	if config == nil {
		absMouseMappingLock.Unlock()
		return fmt.Errorf("config not loaded")
	}
	previous := config.AbsMouseMapping
	config.AbsMouseMapping = &mapping
	absMouseMappingLock.Unlock()
	if err := SaveConfig(); err != nil {
		// Keep in-memory state consistent with what the caller was told:
		// the update failed, so restore the previous mapping.
		absMouseMappingLock.Lock()
		config.AbsMouseMapping = previous
		absMouseMappingLock.Unlock()
		return fmt.Errorf("failed to save config: %w", err)
	}
	logger.Info().
		Bool("enabled", mapping.Enabled).
		Int("totalWidth", mapping.TotalWidth).
		Int("totalHeight", mapping.TotalHeight).
		Int("screenX", mapping.ScreenX).
		Int("screenY", mapping.ScreenY).
		Int("screenWidth", mapping.ScreenWidth).
		Int("screenHeight", mapping.ScreenHeight).
		Msg("absolute mouse mapping updated")
	return nil
}

// HTTP endpoints (authenticated) so host-side automation can keep the mapping
// in sync with display layout changes without a WebRTC session.
func handleGetMouseMapping(c *gin.Context) {
	m := currentAbsMouseMapping()
	if m == nil {
		c.JSON(http.StatusOK, AbsMouseMappingConfig{})
		return
	}
	c.JSON(http.StatusOK, *m)
}

func handleSetMouseMapping(c *gin.Context) {
	var mapping AbsMouseMappingConfig
	if err := c.ShouldBindJSON(&mapping); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Client errors (invalid mapping) are 400; persistence failures are 500.
	if err := mapping.validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := rpcSetAbsMouseMapping(mapping); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
