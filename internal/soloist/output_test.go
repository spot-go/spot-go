package soloist

import (
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		line string
		want event
	}{
		{"2026-11-07 16:55:43.791: soloist 1.3.10.51 build 1793988108 (20261106) (g19412df6ea) (macos/aarch64)", other},
		{"2026-11-07 16:55:43.791: client expires in 88 days", other},
		{"2026-11-07 16:55:43.837: ready, device_name=spot-go-test device_id=84680ce4-e21b-44ea-96eb-921b6f18fc63", ready},
		{"2026-11-07 16:55:43.837: restoring session", other},
		{`2026-11-07 16:55:52.825: waiting for login — connect to "spot-go-test" from your Spotify app`, waitingForLogin},
		{"2026-11-07 16:55:44.529: logged in as francisco.cpg", loggedIn},
		{"2026-11-07 16:56:16.721: login failed: make sure --api-key is valid", loginFailed},
		{`Error: another session is running for data directory "/tmp/data".`, failed},
		{"Stop the running session before starting soloist again.", other},
	}

	for _, tt := range tests {
		if got := classify(tt.line); got != tt.want {
			t.Errorf("classify(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestMessage(t *testing.T) {
	got := message("2026-11-07 16:56:16.721: login failed: make sure --api-key is valid")
	if want := "login failed: make sure --api-key is valid"; got != want {
		t.Fatalf("message() = %q, want %q", got, want)
	}
}

func TestDeviceID(t *testing.T) {
	id, ok := deviceID("2026-11-07 16:55:43.837: ready, device_name=spot-go@host device_id=84680ce4-e21b-44ea-96eb-921b6f18fc63")
	if !ok || id != "84680ce4-e21b-44ea-96eb-921b6f18fc63" {
		t.Fatalf("deviceID() = %q, %v", id, ok)
	}

	if _, ok := deviceID("2026-11-07 16:55:43.837: running"); ok {
		t.Fatal("deviceID() found an id in a line without one")
	}
}

func TestParseBuild(t *testing.T) {
	b, ok := parseBuild("soloist 1.3.10.51 build 1793988108 (20261106) (g19412df6ea) (macos/aarch64)")
	if !ok {
		t.Fatal("parseBuild() did not parse the version output")
	}
	if b.Version != "1.3.10.51" {
		t.Errorf("Version = %q", b.Version)
	}
	if !b.Built.Equal(time.Unix(1793988108, 0)) {
		t.Errorf("Built = %v", b.Built)
	}
	if want := time.Unix(1793988108, 0).Add(90 * 24 * time.Hour); !b.Expires().Equal(want) {
		t.Errorf("Expires() = %v, want %v", b.Expires(), want)
	}

	if _, ok := parseBuild("other-player 0.4.2"); ok {
		t.Fatal("parseBuild() accepted another program's version")
	}
}
