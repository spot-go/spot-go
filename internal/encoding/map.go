package encoding

import (
	"bytes"
	"encoding/gob"
)

func MapToBytes[E any](m []E) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(m)
	return buf.Bytes(), err
}

func BytesToMap[E any](b []byte) ([]E, error) {
	var m []E
	dec := gob.NewDecoder(bytes.NewReader(b))
	err := dec.Decode(&m)
	return m, err
}
