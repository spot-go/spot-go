package main

import (
	"github.com/spot-go/spot-go/cmd"
	"github.com/spot-go/spot-go/internal/media"
)

// version is spot-go's version. The release build sets it, with
// -ldflags "-X main.version=1.2.3".
var version string

func main() {
	cmd.SetVersion(version)
	media.Main(cmd.Execute)
}
