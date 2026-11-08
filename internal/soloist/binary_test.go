package soloist

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDownloadURL(t *testing.T) {
	for platform, archive := range archives {
		if !strings.HasSuffix(archive, ".tar.gz") {
			t.Errorf("%s uses %s, InstallArchive only reads .tar.gz", platform, archive)
		}
	}

	url, ok := DownloadURL()
	if ok != Supported() {
		t.Fatalf("DownloadURL() ok = %v, Supported() = %v", ok, Supported())
	}
	if ok && !strings.HasPrefix(url, "https://soloist-builds.spotifycdn.com/soloist_release_") {
		t.Fatalf("DownloadURL() = %s", url)
	}
}

func TestCheckBinary(t *testing.T) {
	useTempFolder(t)

	if _, err := CheckBinary(); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("CheckBinary() without a binary = %v, want %v", err, ErrNotInstalled)
	}
	if !NeedsDownload() {
		t.Fatal("NeedsDownload() = false without a binary")
	}

	built := time.Now().Add(-time.Hour)
	writeScript(t, binaryFile, fakeScript(built, "exit 0"))
	b, err := CheckBinary()
	if err != nil {
		t.Fatalf("CheckBinary() = %v", err)
	}
	if b.Version != "1.3.10.51" || b.Built.Unix() != built.Unix() {
		t.Fatalf("CheckBinary() = %+v", b)
	}
	if NeedsDownload() {
		t.Fatal("NeedsDownload() = true for a fresh build")
	}

	writeScript(t, binaryFile, fakeScript(time.Now().Add(-80*24*time.Hour), "exit 0"))
	if _, err := CheckBinary(); err != nil {
		t.Fatalf("CheckBinary() for a build expiring in 10 days = %v", err)
	}
	if !NeedsDownload() {
		t.Fatal("NeedsDownload() = false for a build expiring in 10 days")
	}

	writeScript(t, binaryFile, fakeScript(time.Now().Add(-91*24*time.Hour), "exit 0"))
	if _, err := CheckBinary(); !errors.Is(err, ErrExpired) {
		t.Fatalf("CheckBinary() for an expired build = %v, want %v", err, ErrExpired)
	}
}

func TestInstallArchive(t *testing.T) {
	useTempFolder(t)
	stubSignature(t, nil)

	script := fakeScript(time.Now(), "exit 0")
	archive := writeArchive(t, map[string]string{
		"CHANGELOG.md": "# Soloist Changelog\n",
		"soloist":      script,
	})

	if err := InstallArchive(archive); err != nil {
		t.Fatalf("InstallArchive() = %v", err)
	}
	b, err := os.ReadFile(binaryFile)
	if err != nil || string(b) != script {
		t.Fatalf("installed binary = %q, %v", b, err)
	}
	info, err := os.Stat(binaryFile)
	if err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatalf("installed binary is not executable: %v %v", info.Mode(), err)
	}
	if _, err := os.Stat(binaryFile + ".download"); !os.IsNotExist(err) {
		t.Fatalf("the temporary download was left behind: %v", err)
	}
}

func TestInstallArchiveKeepsCurrentBinaryOnFailure(t *testing.T) {
	useTempFolder(t)

	current := fakeScript(time.Now(), "exit 0")
	writeScript(t, binaryFile, current)

	tests := []struct {
		name      string
		signature error
		files     map[string]string
		want      string
	}{
		{
			name:      "unsigned",
			signature: errors.New("the downloaded Soloist is not signed by Spotify"),
			files:     map[string]string{"soloist": fakeScript(time.Now(), "exit 0")},
			want:      "not signed by Spotify",
		},
		{
			name:  "expired",
			files: map[string]string{"soloist": fakeScript(time.Now().Add(-100*24*time.Hour), "exit 0")},
			want:  "expired on",
		},
		{
			name:  "no binary",
			files: map[string]string{"CHANGELOG.md": "# Soloist Changelog\n"},
			want:  "soloist not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubSignature(t, tt.signature)

			err := InstallArchive(writeArchive(t, tt.files))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("InstallArchive() = %v, want an error with %q", err, tt.want)
			}
			b, _ := os.ReadFile(binaryFile)
			if string(b) != current {
				t.Fatal("a failed install replaced the current binary")
			}
		})
	}
}
