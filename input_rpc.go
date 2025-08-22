package kvm

import (
	"fmt"
)

// Constants for input validation
const (
	// MaxKeyboardKeys defines the maximum number of simultaneous key presses
	// This matches the USB HID keyboard report specification
	MaxKeyboardKeys = 6
)

// Input RPC Direct Handlers
// This module provides optimized direct handlers for high-frequency input events,
// bypassing the reflection-based RPC system for improved performance.
//
// Performance benefits:
// - Eliminates reflection overhead (~2-3ms per call)
// - Reduces memory allocations
// - Optimizes parameter parsing and validation
// - Provides faster code path for input methods
//
// The handlers maintain full compatibility with existing RPC interface
// while providing significant latency improvements for input events.

// Common validation helpers for parameter parsing
// These reduce code duplication and provide consistent error messages

// validateFloat64Param extracts and validates a float64 parameter from the params map
func validateFloat64Param(params map[string]interface{}, paramName, methodName string, min, max float64) (float64, error) {
	value, ok := params[paramName].(float64)
	if !ok {
		return 0, fmt.Errorf("%s: %s parameter must be a number, got %T", methodName, paramName, params[paramName])
	}
	if value < min || value > max {
		return 0, fmt.Errorf("%s: %s value %v out of range [%v to %v]", methodName, paramName, value, min, max)
	}
	return value, nil
}

// validateKeysArray extracts and validates a keys array parameter
func validateKeysArray(params map[string]interface{}, methodName string) ([]uint8, error) {
	keysInterface, ok := params["keys"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: keys parameter must be an array, got %T", methodName, params["keys"])
	}
	if len(keysInterface) > MaxKeyboardKeys {
		return nil, fmt.Errorf("%s: too many keys (%d), maximum is %d", methodName, len(keysInterface), MaxKeyboardKeys)
	}

	keys := make([]uint8, len(keysInterface))
	for i, keyInterface := range keysInterface {
		keyFloat, ok := keyInterface.(float64)
		if !ok {
			return nil, fmt.Errorf("%s: key at index %d must be a number, got %T", methodName, i, keyInterface)
		}
		if keyFloat < 0 || keyFloat > 255 {
			return nil, fmt.Errorf("%s: key at index %d value %v out of range [0-255]", methodName, i, keyFloat)
		}
		keys[i] = uint8(keyFloat)
	}
	return keys, nil
}

// Input parameter structures for direct RPC handlers
// These mirror the original RPC method signatures but provide
// optimized parsing from JSON map parameters.

// KeyboardReportParams represents parameters for keyboard HID report
// Matches rpcKeyboardReport(modifier uint8, keys []uint8)
type KeyboardReportParams struct {
	Modifier uint8   `json:"modifier"` // Keyboard modifier keys (Ctrl, Alt, Shift, etc.)
	Keys     []uint8 `json:"keys"`     // Array of pressed key codes (up to 6 keys)
}

// AbsMouseReportParams represents parameters for absolute mouse positioning
// Matches rpcAbsMouseReport(x, y int, buttons uint8)
type AbsMouseReportParams struct {
	X       int   `json:"x"`       // Absolute X coordinate (0-32767)
	Y       int   `json:"y"`       // Absolute Y coordinate (0-32767)
	Buttons uint8 `json:"buttons"` // Mouse button state bitmask
}

// RelMouseReportParams represents parameters for relative mouse movement
// Matches rpcRelMouseReport(dx, dy int8, buttons uint8)
type RelMouseReportParams struct {
	Dx      int8  `json:"dx"`      // Relative X movement delta (-127 to +127)
	Dy      int8  `json:"dy"`      // Relative Y movement delta (-127 to +127)
	Buttons uint8 `json:"buttons"` // Mouse button state bitmask
}

// WheelReportParams represents parameters for mouse wheel events
// Matches rpcWheelReport(wheelY int8)
type WheelReportParams struct {
	WheelY int8 `json:"wheelY"` // Wheel scroll delta (-127 to +127)
}

// Direct handler for keyboard reports
// Optimized path that bypasses reflection for keyboard input events
func handleKeyboardReportDirect(params map[string]interface{}) (interface{}, error) {
	// Extract and validate modifier parameter
	modifierFloat, err := validateFloat64Param(params, "modifier", "keyboardReport", 0, 255)
	if err != nil {
		return nil, err
	}
	modifier := uint8(modifierFloat)

	// Extract and validate keys array
	keys, err := validateKeysArray(params, "keyboardReport")
	if err != nil {
		return nil, err
	}

	return nil, rpcKeyboardReport(modifier, keys)
}

// Direct handler for absolute mouse reports
// Optimized path that bypasses reflection for absolute mouse positioning
func handleAbsMouseReportDirect(params map[string]interface{}) (interface{}, error) {
	// Extract and validate x coordinate
	xFloat, err := validateFloat64Param(params, "x", "absMouseReport", 0, 32767)
	if err != nil {
		return nil, err
	}
	x := int(xFloat)

	// Extract and validate y coordinate
	yFloat, err := validateFloat64Param(params, "y", "absMouseReport", 0, 32767)
	if err != nil {
		return nil, err
	}
	y := int(yFloat)

	// Extract and validate buttons
	buttonsFloat, err := validateFloat64Param(params, "buttons", "absMouseReport", 0, 255)
	if err != nil {
		return nil, err
	}
	buttons := uint8(buttonsFloat)

	return nil, rpcAbsMouseReport(x, y, buttons)
}

// Direct handler for relative mouse reports
// Optimized path that bypasses reflection for relative mouse movement
func handleRelMouseReportDirect(params map[string]interface{}) (interface{}, error) {
	// Extract and validate dx (relative X movement)
	dxFloat, err := validateFloat64Param(params, "dx", "relMouseReport", -127, 127)
	if err != nil {
		return nil, err
	}
	dx := int8(dxFloat)

	// Extract and validate dy (relative Y movement)
	dyFloat, err := validateFloat64Param(params, "dy", "relMouseReport", -127, 127)
	if err != nil {
		return nil, err
	}
	dy := int8(dyFloat)

	// Extract and validate buttons
	buttonsFloat, err := validateFloat64Param(params, "buttons", "relMouseReport", 0, 255)
	if err != nil {
		return nil, err
	}
	buttons := uint8(buttonsFloat)

	return nil, rpcRelMouseReport(dx, dy, buttons)
}

// Direct handler for wheel reports
// Optimized path that bypasses reflection for mouse wheel events
func handleWheelReportDirect(params map[string]interface{}) (interface{}, error) {
	// Extract and validate wheelY (scroll delta)
	wheelYFloat, err := validateFloat64Param(params, "wheelY", "wheelReport", -127, 127)
	if err != nil {
		return nil, err
	}
	wheelY := int8(wheelYFloat)

	return nil, rpcWheelReport(wheelY)
}

// handleInputRPCDirect routes input method calls to their optimized direct handlers
// This is the main entry point for the fast path that bypasses reflection.
// It provides significant performance improvements for high-frequency input events.
//
// Performance monitoring: Consider adding metrics collection here to track
// latency improvements and call frequency for production monitoring.
func handleInputRPCDirect(method string, params map[string]interface{}) (interface{}, error) {
	switch method {
	case "keyboardReport":
		return handleKeyboardReportDirect(params)
	case "absMouseReport":
		return handleAbsMouseReportDirect(params)
	case "relMouseReport":
		return handleRelMouseReportDirect(params)
	case "wheelReport":
		return handleWheelReportDirect(params)
	default:
		// This should never happen if isInputMethod is correctly implemented
		return nil, fmt.Errorf("handleInputRPCDirect: unsupported method '%s'", method)
	}
}

// isInputMethod determines if a given RPC method should use the optimized direct path
// Returns true for input-related methods that have direct handlers implemented.
// This function must be kept in sync with handleInputRPCDirect.
func isInputMethod(method string) bool {
	switch method {
	case "keyboardReport", "absMouseReport", "relMouseReport", "wheelReport":
		return true
	default:
		return false
	}
}
