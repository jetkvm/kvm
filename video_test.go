//go:build linux && arm

package kvm

import (
	"testing"

	"github.com/jetkvm/kvm/internal/native"
)

func TestUnsupportedResolutionState(t *testing.T) {
	tests := []struct {
		name  string
		input native.VideoState
		want  native.VideoState
	}{
		{
			name:  "1920x1080 no error unchanged",
			input: native.VideoState{Ready: true, Width: 1920, Height: 1080},
			want:  native.VideoState{Ready: true, Width: 1920, Height: 1080},
		},
		{
			name:  "3840x2160 no error becomes out_of_range",
			input: native.VideoState{Ready: true, Width: 3840, Height: 2160},
			want:  native.VideoState{Ready: false, Error: "out_of_range", Width: 3840, Height: 2160},
		},
		{
			name:  "1920x1080 no_signal unchanged",
			input: native.VideoState{Ready: false, Error: "no_signal", Width: 1920, Height: 1080},
			want:  native.VideoState{Ready: false, Error: "no_signal", Width: 1920, Height: 1080},
		},
		{
			name:  "3840x2160 no_lock keeps no_lock",
			input: native.VideoState{Ready: false, Error: "no_lock", Width: 3840, Height: 2160},
			want:  native.VideoState{Ready: false, Error: "no_lock", Width: 3840, Height: 2160},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unsupportedResolutionState(tt.input)
			if got != tt.want {
				t.Errorf("unsupportedResolutionState(%+v) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}
