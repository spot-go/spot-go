package cmd

import (
	"runtime/debug"
	"strings"
)

// SetVersion sets the version `spot-go --version` prints: v, which a release
// build sets, or else the version of the module go install built, or else
// dev.
func SetVersion(v string) {
	rootCmd.Version = version(v, debug.ReadBuildInfo)
}

// version returns v, or else the version of the main module that
// readBuildInfo reports without its v, as go install sets it, such as 1.2.3
// for github.com/spot-go/spot-go@v1.2.3, or else dev.
func version(v string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if v != "" {
		return v
	}
	if info, ok := readBuildInfo(); ok {
		if m := info.Main.Version; m != "" && m != "(devel)" {
			return strings.TrimPrefix(m, "v")
		}
	}
	return "dev"
}
