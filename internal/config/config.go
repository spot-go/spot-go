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

	err = configdir.MakePath(Path)
	if err != nil {
		return fmt.Errorf("fail to create config dir: %w", err)
	}

	if path.Exists(configFile) {
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

	err = os.WriteFile(configFile, b, os.ModePerm)
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
