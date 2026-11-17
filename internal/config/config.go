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

	// The new file takes the place of the old one at once, so a spot-go
	// killed while it saves, as spot-go in the background can be, leaves
	// the old one whole. It holds the Spotify token, so only the user reads
	// it.
	f, err := os.CreateTemp(Path, "settings-*.json")
	if err != nil {
		return fmt.Errorf("fail to write file: %+v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return fmt.Errorf("fail to write file: %+v", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("fail to write file: %+v", err)
	}
	if err := os.Rename(f.Name(), configFile); err != nil {
		return fmt.Errorf("fail to write file: %+v", err)
	}
	return nil
}

func (s Settings) NeedsAuthenticateWebSpotify(currentScopes []string) bool {
	return s.Auth == nil ||
		s.Auth.Token == nil ||
		!slices.Equal(s.Auth.Scopes, currentScopes)
}
