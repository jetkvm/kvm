package kvm

import (
	"encoding/json"
	"testing"
)

func TestVideoQualityDefaultsAndSavedChoices(t *testing.T) {
	if got := getDefaultConfig().VideoQualityFactor; got != 0 {
		t.Fatalf("new configurations must default to Auto, got %g", got)
	}
	for _, tt := range []struct {
		name string
		json string
		want float64
	}{
		{"missing setting uses Auto", `{}`, 0},
		{"saved Auto", `{"video_quality_factor":0}`, 0},
		{"saved High", `{"video_quality_factor":1}`, 1},
		{"saved Medium", `{"video_quality_factor":0.5}`, 0.5},
		{"saved Low", `{"video_quality_factor":0.1}`, 0.1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// LoadConfig overlays saved settings on the defaults in this order.
			loaded := getDefaultConfig()
			if err := json.Unmarshal([]byte(tt.json), &loaded); err != nil {
				t.Fatal(err)
			}
			if loaded.VideoQualityFactor != tt.want {
				t.Fatalf("got %g, want %g", loaded.VideoQualityFactor, tt.want)
			}
		})
	}
}
