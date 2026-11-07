package tar

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name     string
	typeflag byte
	content  string
}

func writeArchive(t *testing.T, entries ...entry) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "archive.tar.gz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: 0644, Size: int64(len(e.content)), Typeflag: e.typeflag}
		if e.typeflag == tar.TypeDir {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestExtractFile(t *testing.T) {
	tests := []struct {
		name     string
		entries  []entry
		existing string
		want     string
	}{
		{
			name:    "root entry",
			entries: []entry{{"spotifyd", tar.TypeReg, "new binary"}},
			want:    "new binary",
		},
		{
			name:    "dot slash entry",
			entries: []entry{{"./spotifyd", tar.TypeReg, "new binary"}},
			want:    "new binary",
		},
		{
			name: "skips other entries",
			entries: []entry{
				{"README.md", tar.TypeReg, "readme"},
				{"spotifyd", tar.TypeDir, ""},
				{"../spotifyd", tar.TypeReg, "escape"},
				{"spotifyd", tar.TypeReg, "new binary"},
			},
			want: "new binary",
		},
		{
			name:     "replaces a larger file",
			entries:  []entry{{"spotifyd", tar.TypeReg, "new binary"}},
			existing: "previous binary that is much longer than the new one",
			want:     "new binary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := writeArchive(t, tt.entries...)
			dir := t.TempDir()
			dst := filepath.Join(dir, "spotifyd")
			if tt.existing != "" {
				if err := os.WriteFile(dst, []byte(tt.existing), 0600); err != nil {
					t.Fatal(err)
				}
			}

			if err := ExtractFile(archive, "spotifyd", dst, 0755); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("content = %q, want %q", got, tt.want)
			}

			info, err := os.Stat(dst)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0755 {
				t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
			}

			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Fatalf("leftover files in %s: %v", dir, files)
			}
		})
	}
}

func TestExtractFileMissingEntry(t *testing.T) {
	archive := writeArchive(t, entry{"other", tar.TypeReg, "x"})
	dst := filepath.Join(t.TempDir(), "spotifyd")

	err := ExtractFile(archive, "spotifyd", dst, 0755)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ExtractFile() = %v, want not found", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("destination was created: %v", err)
	}
}
