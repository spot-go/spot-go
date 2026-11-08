//go:build !darwin

package soloist

func platformVerifySignature(string) error {
	return nil
}
