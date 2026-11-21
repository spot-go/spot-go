package cmd

import (
	"runtime/debug"
	"testing"
)

func TestVersion(t *testing.T) {
	built := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Path: "github.com/spot-go/spot-go", Version: v}}, true
		}
	}
	noInfo := func() (*debug.BuildInfo, bool) { return nil, false }

	tests := []struct {
		name          string
		v             string
		readBuildInfo func() (*debug.BuildInfo, bool)
		want          string
	}{
		{"release build", "1.2.3", built("(devel)"), "1.2.3"},
		{"release build over the module version", "1.2.3", built("v1.2.2"), "1.2.3"},
		{"go install of a tag", "", built("v1.2.3"), "1.2.3"},
		{"go install of a commit", "", built("v0.0.0-20261121160834-4f3d9f7a1b2c"), "0.0.0-20261121160834-4f3d9f7a1b2c"},
		{"go run", "", built("(devel)"), "dev"},
		{"no module version", "", built(""), "dev"},
		{"no build info", "", noInfo, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := version(tt.v, tt.readBuildInfo); got != tt.want {
				t.Fatalf("version(%q) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}
