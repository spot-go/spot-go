package soloist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifySignatureRejectsUnsignedBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "soloist")
	writeScript(t, bin, fakeScript(time.Now(), "exit 0"))

	err := platformVerifySignature(bin)
	if err == nil || !strings.Contains(err.Error(), "not signed by Spotify") {
		t.Fatalf("platformVerifySignature() = %v, want a signature error", err)
	}
}

func TestVerifySignatureAcceptsSoloist(t *testing.T) {
	bin := os.Getenv("SOLOIST_BINARY")
	if bin == "" {
		t.Skip("set SOLOIST_BINARY to an official Soloist binary to run this test")
	}

	if err := platformVerifySignature(bin); err != nil {
		t.Fatalf("platformVerifySignature(%s) = %v", bin, err)
	}
}
