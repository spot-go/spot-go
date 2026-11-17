package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func TestSaveReplacesTheFileWhole(t *testing.T) {
	oldPath, oldFile, oldSettings := Path, configFile, AppSettings
	t.Cleanup(func() { Path, configFile, AppSettings = oldPath, oldFile, oldSettings })
	Path = t.TempDir()
	configFile = filepath.Join(Path, "settings.json")
	if err := os.WriteFile(configFile, []byte(`{"auth":{"oauth2Token":{"access_token":"old"}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	AppSettings = &Settings{Auth: &Auth{}}
	if err := SaveToken(&oauth2.Token{AccessToken: "new", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}

	AppSettings = nil
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if got := AppSettings.Auth.Token.RefreshToken; got != "refresh" {
		t.Fatalf("saved refresh token = %q, want refresh", got)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("settings.json mode = %v, want 0600: it holds the Spotify token", mode)
	}
	entries, err := os.ReadDir(Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the config folder has %d files, want only settings.json", len(entries))
	}
}
