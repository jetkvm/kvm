package utils

import (
	"reflect"
	"testing"

	"github.com/Masterminds/semver/v3"
)

type nativeOptions struct {
	Disable              bool            `env:"JETKVM_NATIVE_DISABLE"`
	SystemVersion        *semver.Version `env:"JETKVM_NATIVE_SYSTEM_VERSION"`
	AppVersion           *semver.Version `env:"JETKVM_NATIVE_APP_VERSION"`
	DisplayRotation      uint16          `env:"JETKVM_NATIVE_DISPLAY_ROTATION"`
	DefaultQualityFactor float64         `env:"JETKVM_NATIVE_DEFAULT_QUALITY_FACTOR"`
}

func TestMarshalEnv(t *testing.T) {
	tests := []struct {
		name     string
		instance interface{}
		want     []string
		wantErr  bool
	}{
		{
			name: "basic struct",
			instance: nativeOptions{
				Disable:              false,
				SystemVersion:        semver.MustParse("1.1.0"),
				AppVersion:           semver.MustParse("1111.0.0"),
				DisplayRotation:      1,
				DefaultQualityFactor: 1.0,
			},
			want: []string{
				"JETKVM_NATIVE_DISABLE=false",
				"JETKVM_NATIVE_SYSTEM_VERSION=1.1.0",
				"JETKVM_NATIVE_APP_VERSION=1111.0.0",
				"JETKVM_NATIVE_DISPLAY_ROTATION=1",
				"JETKVM_NATIVE_DEFAULT_QUALITY_FACTOR=1",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalEnv(tt.instance)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
