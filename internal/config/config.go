package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kirsle/configdir"
	"github.com/spot-go/spot-go/internal/path"
	"golang.org/x/oauth2"
)

type (
	Settings struct {
		Token *oauth2.Token `json:"oauth2Token"`
	}
)

const (
	appName = "spot-go"
)

var (
	configPath = configdir.LocalConfig(appName)
	configFile = filepath.Join(configPath, "settings.json")

	AppSettings *Settings
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

	AppSettings = &Settings{}
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
