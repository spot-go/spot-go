package main

import (
	"github.com/spot-go/spot-go/cmd"
	"github.com/spot-go/spot-go/internal/media"
)

func main() {
	media.Main(cmd.Execute)
}
