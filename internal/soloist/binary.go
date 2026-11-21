package soloist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/franciscocpg/spot-go/internal/path"
	"github.com/franciscocpg/spot-go/internal/tar"
)

const (
	DownloadsPage = "https://developer.spotify.com/documentation/soloist/reference/downloads-and-updates"

	downloadBaseURL = "https://soloist-builds.spotifycdn.com/"
	updateBefore    = 14 * 24 * time.Hour
	versionTimeout  = 10 * time.Second
)

var (
	ErrNotInstalled = errors.New("Soloist is not installed")
	ErrExpired      = errors.New("the Soloist build expired, a newer one has to be downloaded")

	archives = map[string]string{
		"darwin/arm64": "soloist_release_osx_arm64.tar.gz",
		"linux/amd64":  "soloist_release_x86_64.tar.gz",
		"linux/arm64":  "soloist_release_arm64.tar.gz",
		"linux/arm":    "soloist_release_arm32.tar.gz",
	}

	verifySignature = platformVerifySignature
)

func Supported() bool {
	_, ok := archives[platform()]
	return ok
}

func DownloadURL() (string, bool) {
	archive, ok := archives[platform()]
	if !ok {
		return "", false
	}
	return downloadBaseURL + archive, true
}

func CheckBinary() (Build, error) {
	if !path.Exists(binaryFile) {
		return Build{}, ErrNotInstalled
	}

	b, err := readBuild(binaryFile)
	if err != nil {
		return Build{}, err
	}
	if !time.Now().Before(b.Expires()) {
		return b, ErrExpired
	}
	return b, nil
}

func NeedsDownload() bool {
	b, err := CheckBinary()
	return err != nil || time.Until(b.Expires()) < updateBefore
}

func InstallArchive(file string) error {
	tmp := binaryFile + ".download"
	defer os.Remove(tmp)

	if err := tar.ExtractFile(file, "soloist", tmp, 0755); err != nil {
		return err
	}
	if err := verifySignature(tmp); err != nil {
		return err
	}

	b, err := readBuild(tmp)
	if err != nil {
		return err
	}
	if !time.Now().Before(b.Expires()) {
		return fmt.Errorf("the downloaded Soloist %s expired on %s", b.Version, b.Expires().Format("2006-01-02"))
	}

	if err := os.Rename(tmp, binaryFile); err != nil {
		return fmt.Errorf("fail to install Soloist: %w", err)
	}
	return nil
}

func readBuild(bin string) (Build, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return Build{}, fmt.Errorf("fail to run %s --version: %v\n%s", bin, err, text)
	}

	b, ok := parseBuild(text)
	if !ok {
		return Build{}, fmt.Errorf("unexpected output from %s --version: %s", bin, text)
	}
	return b, nil
}

func platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
