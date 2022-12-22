package hash

import (
	"crypto/sha512"
	"fmt"
	"io"
	"os"
)

func SHA512Sum(file string) string {
	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		panic(err)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}
