package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/kirsle/configdir"
	"github.com/spot-go/spot-go/internal/path"

	"golang.org/x/oauth2"
)

type (
	Auth struct {
		Token  *oauth2.Token `json:"oauth2Token"`
		Scopes []string      `json:"scopes"`
		// ClientID is the Client ID of the Spotify app Token is of. The
		// logins spot-go saved before it took other apps than its own have
		// none.
		ClientID string `json:"clientID,omitempty"`
	}

	Settings struct {
		Auth *Auth `json:"auth"`
	}
)

const (
	appName = "spot-go"
)

var (
	Path        = configdir.LocalConfig(appName)
	configFile  = filepath.Join(Path, "settings.json")
	LibraryFile = filepath.Join(Path, "library.json")

	AppSettings *Settings
	Hostname    string

	saveMu sync.Mutex
)

func Load() error {
	var err error
	Hostname, err = os.Hostname()
	if err != nil {
		return fmt.Errorf("fail to get hostname: %w", err)
	}

	// Only the user may open the folder, which has the Spotify login. spot-go
	// made it and settings.json readable to every user of the computer
	// before, so they are made private here too when they already exist.
	if err := os.MkdirAll(Path, 0700); err != nil {
		return fmt.Errorf("fail to create config dir: %w", err)
	}
	if err := os.Chmod(Path, 0700); err != nil {
		return fmt.Errorf("fail to protect config dir: %w", err)
	}

	if path.Exists(configFile) {
		if err := os.Chmod(configFile, 0600); err != nil {
			return fmt.Errorf("fail to protect config file: %w", err)
		}
		b, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("fail to read config file: %w", err)
		}

		// Ignore invalid json
		err = json.Unmarshal(b, &AppSettings)
		if err == nil {
			return nil
		}
	}

	AppSettings = &Settings{Auth: &Auth{}}
	return AppSettings.Save()
}

func (s *Settings) Save() error {
	saveMu.Lock()
	defer saveMu.Unlock()

	return s.save()
}

// SaveAuth saves t, a new token of the Spotify app clientID with scopes,
// in place of the login saved before.
func SaveAuth(t *oauth2.Token, scopes []string, clientID string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	if AppSettings.Auth == nil {
		AppSettings.Auth = &Auth{}
	}
	AppSettings.Auth.Token = t
	AppSettings.Auth.Scopes = scopes
	AppSettings.Auth.ClientID = clientID
	return AppSettings.save()
}

func SaveToken(t *oauth2.Token) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	AppSettings.Auth.Token = t
	return AppSettings.save()
}

func (s *Settings) save() error {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("fail to marshal json: %+v", err)
	}

	// A new file takes the token, so whoever opened settings.json while
	// others could read it doesn't get the new one.
	err = path.WriteFile(configFile, b)
	if err != nil {
		return fmt.Errorf("fail to write file: %+v", err)
	}

	return nil
}

func (s Settings) NeedsAuthenticateWebSpotify(currentScopes []string) bool {
	return s.Auth == nil ||
		s.Auth.Token == nil ||
		!slices.Equal(s.Auth.Scopes, currentScopes)
}
