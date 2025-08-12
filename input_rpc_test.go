package kvm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test validateFloat64Param function
func TestValidateFloat64Param(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		paramName   string
		methodName  string
		min         float64
		max         float64
		expected    float64
		expectError bool
	}{
		{
			name:        "valid parameter",
			params:      map[string]interface{}{"test": 50.0},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    50.0,
			expectError: false,
		},
		{
			name:        "parameter at minimum boundary",
			params:      map[string]interface{}{"test": 0.0},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    0.0,
			expectError: false,
		},
		{
			name:        "parameter at maximum boundary",
			params:      map[string]interface{}{"test": 100.0},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    100.0,
			expectError: false,
		},
		{
			name:        "parameter below minimum",
			params:      map[string]interface{}{"test": -1.0},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    0,
			expectError: true,
		},
		{
			name:        "parameter above maximum",
			params:      map[string]interface{}{"test": 101.0},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    0,
			expectError: true,
		},
		{
			name:        "wrong parameter type",
			params:      map[string]interface{}{"test": "not a number"},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    0,
			expectError: true,
		},
		{
			name:        "missing parameter",
			params:      map[string]interface{}{},
			paramName:   "test",
			methodName:  "testMethod",
			min:         0,
			max:         100,
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateFloat64Param(tt.params, tt.paramName, tt.methodName, tt.min, tt.max)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// Test validateKeysArray function
func TestValidateKeysArray(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		methodName  string
		expected    []uint8
		expectError bool
	}{
		{
			name:        "valid keys array",
			params:      map[string]interface{}{"keys": []interface{}{65.0, 66.0, 67.0}},
			methodName:  "testMethod",
			expected:    []uint8{65, 66, 67},
			expectError: false,
		},
		{
			name:        "empty keys array",
			params:      map[string]interface{}{"keys": []interface{}{}},
			methodName:  "testMethod",
			expected:    []uint8{},
			expectError: false,
		},
		{
			name:        "maximum keys array",
			params:      map[string]interface{}{"keys": []interface{}{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}},
			methodName:  "testMethod",
			expected:    []uint8{1, 2, 3, 4, 5, 6},
			expectError: false,
		},
		{
			name:        "too many keys",
			params:      map[string]interface{}{"keys": []interface{}{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0}},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid key type",
			params:      map[string]interface{}{"keys": []interface{}{"not a number"}},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "key value out of range (negative)",
			params:      map[string]interface{}{"keys": []interface{}{-1.0}},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "key value out of range (too high)",
			params:      map[string]interface{}{"keys": []interface{}{256.0}},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "wrong parameter type",
			params:      map[string]interface{}{"keys": "not an array"},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "missing keys parameter",
			params:      map[string]interface{}{},
			methodName:  "testMethod",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateKeysArray(tt.params, tt.methodName)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// Test handleKeyboardReportDirect function
func TestHandleKeyboardReportDirect(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid keyboard report",
			params: map[string]interface{}{
				"modifier": 2.0, // Shift key
				"keys":     []interface{}{65.0, 66.0}, // A, B keys
			},
			expectError: false,
		},
		{
			name: "empty keys array",
			params: map[string]interface{}{
				"modifier": 0.0,
				"keys":     []interface{}{},
			},
			expectError: false,
		},
		{
			name: "invalid modifier",
			params: map[string]interface{}{
				"modifier": 256.0, // Out of range
				"keys":     []interface{}{65.0},
			},
			expectError: true,
		},
		{
			name: "invalid keys",
			params: map[string]interface{}{
				"modifier": 0.0,
				"keys":     []interface{}{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0}, // Too many keys
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleKeyboardReportDirect(tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test handleAbsMouseReportDirect function
func TestHandleAbsMouseReportDirect(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid absolute mouse report",
			params: map[string]interface{}{
				"x":       1000.0,
				"y":       500.0,
				"buttons": 1.0, // Left button
			},
			expectError: false,
		},
		{
			name: "boundary values",
			params: map[string]interface{}{
				"x":       0.0,
				"y":       32767.0,
				"buttons": 255.0,
			},
			expectError: false,
		},
		{
			name: "invalid x coordinate",
			params: map[string]interface{}{
				"x":       -1.0, // Out of range
				"y":       500.0,
				"buttons": 0.0,
			},
			expectError: true,
		},
		{
			name: "invalid y coordinate",
			params: map[string]interface{}{
				"x":       1000.0,
				"y":       32768.0, // Out of range
				"buttons": 0.0,
			},
			expectError: true,
		},
		{
			name: "invalid buttons",
			params: map[string]interface{}{
				"x":       1000.0,
				"y":       500.0,
				"buttons": 256.0, // Out of range
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleAbsMouseReportDirect(tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test handleRelMouseReportDirect function
func TestHandleRelMouseReportDirect(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid relative mouse report",
			params: map[string]interface{}{
				"dx":      10.0,
				"dy":      -5.0,
				"buttons": 2.0, // Right button
			},
			expectError: false,
		},
		{
			name: "boundary values",
			params: map[string]interface{}{
				"dx":      -127.0,
				"dy":      127.0,
				"buttons": 0.0,
			},
			expectError: false,
		},
		{
			name: "invalid dx",
			params: map[string]interface{}{
				"dx":      -128.0, // Out of range
				"dy":      0.0,
				"buttons": 0.0,
			},
			expectError: true,
		},
		{
			name: "invalid dy",
			params: map[string]interface{}{
				"dx":      0.0,
				"dy":      128.0, // Out of range
				"buttons": 0.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleRelMouseReportDirect(tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test handleWheelReportDirect function
func TestHandleWheelReportDirect(t *testing.T) {
	tests := []struct {
		name        string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid wheel report",
			params: map[string]interface{}{
				"wheelY": 3.0,
			},
			expectError: false,
		},
		{
			name: "boundary values",
			params: map[string]interface{}{
				"wheelY": -127.0,
			},
			expectError: false,
		},
		{
			name: "invalid wheelY",
			params: map[string]interface{}{
				"wheelY": 128.0, // Out of range
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleWheelReportDirect(tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test handleInputRPCDirect function
func TestHandleInputRPCDirect(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:   "keyboard report",
			method: "keyboardReport",
			params: map[string]interface{}{
				"modifier": 0.0,
				"keys":     []interface{}{65.0},
			},
			expectError: false,
		},
		{
			name:   "absolute mouse report",
			method: "absMouseReport",
			params: map[string]interface{}{
				"x":       1000.0,
				"y":       500.0,
				"buttons": 1.0,
			},
			expectError: false,
		},
		{
			name:   "relative mouse report",
			method: "relMouseReport",
			params: map[string]interface{}{
				"dx":      10.0,
				"dy":      -5.0,
				"buttons": 2.0,
			},
			expectError: false,
		},
		{
			name:   "wheel report",
			method: "wheelReport",
			params: map[string]interface{}{
				"wheelY": 3.0,
			},
			expectError: false,
		},
		{
			name:        "unknown method",
			method:      "unknownMethod",
			params:      map[string]interface{}{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handleInputRPCDirect(tt.method, tt.params)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test isInputMethod function
func TestIsInputMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		expected bool
	}{
		{
			name:     "keyboard report method",
			method:   "keyboardReport",
			expected: true,
		},
		{
			name:     "absolute mouse report method",
			method:   "absMouseReport",
			expected: true,
		},
		{
			name:     "relative mouse report method",
			method:   "relMouseReport",
			expected: true,
		},
		{
			name:     "wheel report method",
			method:   "wheelReport",
			expected: true,
		},
		{
			name:     "non-input method",
			method:   "someOtherMethod",
			expected: false,
		},
		{
			name:     "empty method",
			method:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInputMethod(tt.method)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests to verify performance improvements
func BenchmarkValidateFloat64Param(b *testing.B) {
	params := map[string]interface{}{"test": 50.0}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validateFloat64Param(params, "test", "benchmarkMethod", 0, 100)
	}
}

func BenchmarkValidateKeysArray(b *testing.B) {
	params := map[string]interface{}{"keys": []interface{}{65.0, 66.0, 67.0}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validateKeysArray(params, "benchmarkMethod")
	}
}

func BenchmarkHandleKeyboardReportDirect(b *testing.B) {
	params := map[string]interface{}{
		"modifier": 2.0,
		"keys":     []interface{}{65.0, 66.0},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handleKeyboardReportDirect(params)
	}
}

func BenchmarkHandleInputRPCDirect(b *testing.B) {
	params := map[string]interface{}{
		"modifier": 2.0,
		"keys":     []interface{}{65.0, 66.0},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handleInputRPCDirect("keyboardReport", params)
	}
}