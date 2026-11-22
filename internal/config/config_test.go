package config

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func useTempFolder(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "Application Support", "spot-go")
	oldPath, oldConfig, oldLibrary, oldSettings := Path, configFile, LibraryFile, AppSettings
	Path = dir
	configFile = filepath.Join(dir, "settings.json")
	LibraryFile = filepath.Join(dir, "library.json")
	t.Cleanup(func() {
		Path, configFile, LibraryFile, AppSettings = oldPath, oldConfig, oldLibrary, oldSettings
	})
	return dir
}

// writeOld writes settings.json and its folder as spot-go did before,
// readable by every user.
func writeOld(t *testing.T, content string) {
	t.Helper()

	if err := os.MkdirAll(Path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	// os.MkdirAll and os.WriteFile leave out what the umask takes away.
	for _, name := range []string{Path, configFile} {
		if err := os.Chmod(name, 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func checkPerm(t *testing.T, name string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != want {
		t.Fatalf("%s mode = %o, want %o", filepath.Base(name), perm, want)
	}
}

func savedToken(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	return tokenIn(t, b)
}

func tokenIn(t *testing.T, b []byte) string {
	t.Helper()

	var s *Settings
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings.json = %s: %v", b, err)
	}
	return accessToken(s)
}

func accessToken(s *Settings) string {
	if s == nil || s.Auth == nil || s.Auth.Token == nil {
		return ""
	}
	return s.Auth.Token.AccessToken
}

func TestLoadMakesPrivateFiles(t *testing.T) {
	dir := useTempFolder(t)

	if err := Load(); err != nil {
		t.Fatalf("Load() = %v", err)
	}
	checkPerm(t, dir, 0700)
	checkPerm(t, configFile, 0600)

	if err := SaveAuth(&oauth2.Token{AccessToken: "new"}, []string{"scope"}, "client"); err != nil {
		t.Fatalf("SaveAuth() = %v", err)
	}
	checkPerm(t, configFile, 0600)
	if got := savedToken(t); got != "new" {
		t.Fatalf("saved token = %q, want new", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("config dir has %v, want only settings.json", entries)
	}
}

func TestLoadProtectsOldFiles(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantToken string
	}{
		{name: "login", content: `{"auth":{"oauth2Token":{"access_token":"old"},"scopes":["scope"]}}`, wantToken: "old"},
		{name: "invalid json", content: `{"auth":`, wantToken: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTempFolder(t)
			writeOld(t, tt.content)

			if err := Load(); err != nil {
				t.Fatalf("Load() = %v", err)
			}
			checkPerm(t, Path, 0700)
			checkPerm(t, configFile, 0600)

			if got := savedToken(t); got != tt.wantToken {
				t.Fatalf("saved token = %q, want %q", got, tt.wantToken)
			}
			if got := accessToken(AppSettings); got != tt.wantToken {
				t.Fatalf("loaded token = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

func TestSaveTokenWritesNewFile(t *testing.T) {
	useTempFolder(t)
	writeOld(t, `{"auth":{"oauth2Token":{"access_token":"old"},"scopes":["scope"]}}`)
	// Another user opened settings.json while spot-go left it readable.
	opened, err := os.Open(configFile)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	if err := Load(); err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if err := SaveToken(&oauth2.Token{AccessToken: "new"}); err != nil {
		t.Fatalf("SaveToken() = %v", err)
	}
	checkPerm(t, configFile, 0600)
	if got := savedToken(t); got != "new" {
		t.Fatalf("saved token = %q, want new", got)
	}

	b, err := io.ReadAll(opened)
	if err != nil {
		t.Fatal(err)
	}
	if got := tokenIn(t, b); got != "old" {
		t.Fatalf("token in the settings.json opened before = %q, want old", got)
	}
}
