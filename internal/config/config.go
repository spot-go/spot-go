package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kirsle/configdir"
	"github.com/spot-go/spot-go/internal/hash"
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

	spotifydFile           = filepath.Join(Path, "spotifyd")
	spotifydExpectedSha512 = map[string]string{
		"linux":  "1cc4897e2394f37273e030b8633a7f6fba6fe232d560f10be03b3a74eba2c001704d40864501b68917b4c24691abf01f7e529c0673167e509467eabe3bab524d",
		"darwin": "eb189206192894f5e11615a0154efaec5226297181f5e2f2fec58b8f2fb47eacd84ba00acb752ca153ada4e81f67bb7ab2adf893e756f3ffda067667e528a77b",
	}
)

func init() {
	configPath := configdir.LocalConfig(appName)
	err := configdir.MakePath(configPath)
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

func IsSpotifydFileValid() bool {
	return hash.SHA512Sum(spotifydFile) == spotifydExpectedSha512[runtime.GOOS]
}

func NeedsDownloadSpotifyd() bool {
	return !path.Exists(spotifydFile) || !IsSpotifydFileValid()
}
