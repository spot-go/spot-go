package spotifyd

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spot-go/spot-go/internal/path"
	"github.com/spot-go/spot-go/internal/spotifyd/process"
)

var (
	platformDaemonArgs []string

	minVersion       = process.Version{Major: 0, Minor: 4, Patch: 2}
	binaryCandidates = []string{
		"/opt/homebrew/bin/spotifyd",
		"/usr/local/bin/spotifyd",
	}

	errDownloadUnsupported = errors.New("spotifyd is not downloaded on macOS, install it with: brew install spotifyd")
)

func CheckBinary() error {
	candidates := binaryCandidates
	if bin, err := exec.LookPath("spotifyd"); err == nil {
		candidates = append([]string{bin}, candidates...)
	}

	var problems []string
	seen := make(map[string]bool)
	for _, bin := range candidates {
		if seen[bin] || !path.Exists(bin) {
			continue
		}
		seen[bin] = true

		v, err := binaryVersion(bin)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !v.AtLeast(minVersion) {
			problems = append(problems, fmt.Sprintf("%s is version %s, run: brew upgrade spotifyd", bin, v))
			continue
		}

		setBinary(bin)
		return nil
	}

	msg := fmt.Sprintf("spotifyd %s or newer was not found. Install it with: brew install spotifyd", minVersion)
	if len(problems) > 0 {
		msg += "\n\n" + strings.Join(problems, "\n")
	}
	return errors.New(msg)
}

func DownloadURL() (string, bool) {
	return "", false
}

func InstallArchive(string) error {
	return errDownloadUnsupported
}
