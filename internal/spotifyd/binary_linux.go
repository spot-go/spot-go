package spotifyd

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/spot-go/spot-go/internal/hash"
	"github.com/spot-go/spot-go/internal/path"
	"github.com/spot-go/spot-go/internal/spotifyd/process"
	"github.com/spot-go/spot-go/internal/tar"
)

type releaseAsset struct {
	url    string
	sha512 string
}

const releaseURL = "https://github.com/Spotifyd/spotifyd/releases/download/v0.4.2/"

var (
	platformDaemonArgs = []string{"--use-mpris=false"}

	requiredVersion = process.Version{Major: 0, Minor: 4, Patch: 2}
	binaryFile      = filepath.Join(ConfigFolder, "spotifyd")
	releaseAssets   = map[string]releaseAsset{
		"amd64": {
			url:    releaseURL + "spotifyd-linux-x86_64-default.tar.gz",
			sha512: "c2b73784bf904d40d9a58f6c43e215a7be448f01039ae899ca5dbe4cdb9369d77e4d43c3a1e63fef7ca5adda365eac37f1d91c1e8018391afa907660552bd8db",
		},
		"arm64": {
			url:    releaseURL + "spotifyd-linux-aarch64-default.tar.gz",
			sha512: "23d7f48a05895b25722c467178d17c6c3f1e67efe0e2eea42a5362d61f0b46927635cc30d61ef64f7e1147523fafbaeda8bda9afe36cebccb704bdaf3d9a61e9",
		},
		"arm": {
			url:    releaseURL + "spotifyd-linux-armv7-default.tar.gz",
			sha512: "0d99651b64602d3d55f40ba325ff880881cf5f039fab33d8f64daddee60910cc762e72d8f4d93497a045f149bb7fb5db1857af80c4a97be2dee5ae0d91a97daa",
		},
	}
)

func CheckBinary() error {
	if !path.Exists(binaryFile) {
		if _, ok := releaseAssets[runtime.GOARCH]; !ok {
			return fmt.Errorf("spotifyd has no prebuilt binary for linux/%s, install spotifyd %s at %s", runtime.GOARCH, requiredVersion, binaryFile)
		}
		return fmt.Errorf("spotifyd is not installed at %s", binaryFile)
	}

	v, err := binaryVersion(binaryFile)
	if err != nil {
		return err
	}
	if v != requiredVersion {
		return fmt.Errorf("%s is version %s, version %s is required", binaryFile, v, requiredVersion)
	}

	setBinary(binaryFile)
	return nil
}

func DownloadURL() (string, bool) {
	asset, ok := releaseAssets[runtime.GOARCH]
	return asset.url, ok
}

func InstallArchive(file string) error {
	asset, ok := releaseAssets[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("spotifyd has no prebuilt binary for linux/%s", runtime.GOARCH)
	}

	if sum := hash.SHA512Sum(file); sum != asset.sha512 {
		return fmt.Errorf("checksum mismatch for %s: got %s", filepath.Base(file), sum)
	}

	return tar.ExtractFile(file, "spotifyd", binaryFile, 0755)
}
