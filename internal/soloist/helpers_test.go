package soloist

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func useTempFolder(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "Application Support", "soloist")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	oldFolder, oldBinary, oldKey, oldData := Folder, binaryFile, apiKeyFile, dataDir
	Folder = dir
	binaryFile = filepath.Join(dir, "soloist")
	apiKeyFile = filepath.Join(dir, "api-key")
	dataDir = filepath.Join(dir, "data")
	t.Cleanup(func() {
		Stop()
		Folder, binaryFile, apiKeyFile, dataDir = oldFolder, oldBinary, oldKey, oldData
	})
	return dir
}

func fakeScript(built time.Time, daemon string) string {
	return fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "soloist 1.3.10.51 build %d (20261106) (g19412df6ea) (macos/aarch64)"
  exit 0
fi
%s
`, built.Unix(), daemon)
}

func writeScript(t *testing.T, file, body string) {
	t.Helper()

	if err := os.WriteFile(file, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}

func writeArchive(t *testing.T, files map[string]string) string {
	t.Helper()

	archive := filepath.Join(t.TempDir(), "soloist_release.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0555, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func stubSignature(t *testing.T, err error) {
	t.Helper()

	old := verifySignature
	verifySignature = func(string) error { return err }
	t.Cleanup(func() { verifySignature = old })
}
