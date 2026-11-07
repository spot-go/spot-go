package tar

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

func ExtractFile(archive, name, dst string, mode os.FileMode) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("fail to open file: %+v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("fail to create reader: %+v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not found in %s", name, filepath.Base(archive))
		}
		if err != nil {
			return fmt.Errorf("fail to read tar file: %+v", err)
		}

		if header.Typeflag == tar.TypeReg && path.Clean(header.Name) == name {
			return writeFile(tr, dst, mode)
		}
	}
}

func writeFile(r io.Reader, dst string, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return fmt.Errorf("fail to create temp file: %+v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fmt.Errorf("fail to copy tar file: %+v", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fail to close temp file: %+v", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("fail to set file mode: %+v", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("fail to move file: %+v", err)
	}
	return nil
}
