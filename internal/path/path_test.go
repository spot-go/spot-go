package path

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	tests := []struct {
		name string
		old  bool
	}{
		{name: "new file"},
		{name: "file others can read", old: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "settings.json")
			if tt.old {
				if err := os.WriteFile(name, []byte("old"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(name, 0755); err != nil {
					t.Fatal(err)
				}
			}

			if err := WriteFile(name, []byte("new")); err != nil {
				t.Fatalf("WriteFile() = %v", err)
			}

			b, err := os.ReadFile(name)
			if err != nil || string(b) != "new" {
				t.Fatalf("file = %q, %v, want new", b, err)
			}
			info, err := os.Lstat(name)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); !info.Mode().IsRegular() || perm != 0600 {
				t.Fatalf("file mode = %v, want a file with 600", info.Mode())
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("dir has %v, want only settings.json", entries)
			}
		})
	}
}

func TestWriteFileKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(link, []byte("new")); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link mode = %v, want a symbolic link", info.Mode())
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "new" {
		t.Fatalf("linked file = %q, %v, want new", b, err)
	}
}
