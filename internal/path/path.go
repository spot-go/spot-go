package path

import (
	"os"
	"path/filepath"
)

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// WriteFile writes b to a new file, which only its owner can open, and puts
// it in the place of name at once. A program that reads name meanwhile, or a
// crash, finds the old content or the new one, never part of it, and one
// that has name open goes on with the old file. When name is a symbolic
// link, the file it links to gets b.
func WriteFile(name string, b []byte) error {
	if target, err := filepath.EvalSymlinks(name); err == nil {
		name = target
	}
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
