package main

import (
	"github.com/franciscocpg/spot-go/cmd"
	"github.com/franciscocpg/spot-go/internal/media"
)

func main() {
	media.Main(cmd.Execute)
}
