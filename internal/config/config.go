package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kirsle/configdir"
	"github.com/spot-go/spot-go/internal/path"

	"golang.org/x/exp/slices"
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
	Path       = configdir.LocalConfig(appName)
	configFile = filepath.Join(Path, "settings.json")

	AppSettings *Settings
	Hostname    string
)

func init() {
	var err error
	Hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}

	configPath := configdir.LocalConfig(appName)
	err = configdir.MakePath(configPath)
	if err != nil {
		panic(err)
	}

	if path.Exists(configFile) {
		b, err := os.ReadFile(configFile)
		if err != nil {
			panic(err)
		}

		// Ignore invalid json
		err = json.Unmarshal(b, &AppSettings)
		if err == nil {
			return
		}
	}

	AppSettings = &Settings{Auth: &Auth{}}
	err = AppSettings.Save()
	if err != nil {
		panic(err)
	}
}

func (s *Settings) Save() error {
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
