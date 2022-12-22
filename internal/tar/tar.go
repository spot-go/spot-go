package tar

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Extract all entries in the tar file to dst
func Extract(file, dst string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("fail to open file: %+v", err)
	}

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("fail to create reader: %+v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil

		case err != nil:
			return err

		case header == nil:
			continue
		}

		target := filepath.Join(dst, header.Name)

		switch header.Typeflag {

		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("fail to open tar file: %+v", err)
			}

			if _, err := io.Copy(f, tr); err != nil && err != io.EOF {
				return fmt.Errorf("fail to copy tar file: %+v", err)
			}

			f.Close()
		}
	}
}
