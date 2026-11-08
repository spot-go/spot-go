package backend

import (
	"fmt"
	"runtime"

	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/spotifyd"
)

type Kind string

const (
	Soloist  Kind = "soloist"
	Spotifyd Kind = "spotifyd"
)

func Default() Kind {
	if soloist.Supported() {
		return Soloist
	}
	return Spotifyd
}

func Parse(s string) (Kind, error) {
	switch Kind(s) {
	case Soloist:
		if !soloist.Supported() {
			return "", fmt.Errorf("Soloist has no build for %s/%s, use --player %s", runtime.GOOS, runtime.GOARCH, Spotifyd)
		}
		return Soloist, nil
	case Spotifyd:
		return Spotifyd, nil
	default:
		return "", fmt.Errorf("unknown player %q, use %s or %s", s, Soloist, Spotifyd)
	}
}

func (k Kind) Name() string {
	if k == Soloist {
		return "Soloist"
	}
	return "spotifyd"
}

func CreateFolders() error {
	if err := spotifyd.CreateConfigFolder(); err != nil {
		return err
	}
	return soloist.CreateFolder()
}

func KillStale() {
	spotifyd.KillStale()
	soloist.KillStale()
}

func Stop() {
	spotifyd.Stop()
	soloist.Stop()
}
