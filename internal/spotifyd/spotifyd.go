package spotifyd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spot-go/spot-go/internal/command"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/hash"
	"github.com/spot-go/spot-go/internal/path"
)

var (
	ConfigFolder             = filepath.Join(config.Path, "spotifyd")
	binaryFile               = filepath.Join(ConfigFolder, "spotifyd")
	credentialsFile          = filepath.Join(ConfigFolder, "credentials.json")
	expectedBinaryFileSha512 = map[string]string{
		"linux":  "1cc4897e2394f37273e030b8633a7f6fba6fe232d560f10be03b3a74eba2c001704d40864501b68917b4c24691abf01f7e529c0673167e509467eabe3bab524d",
		"darwin": "eb189206192894f5e11615a0154efaec5226297181f5e2f2fec58b8f2fb47eacd84ba00acb752ca153ada4e81f67bb7ab2adf893e756f3ffda067667e528a77b",
	}

	deviceName = fmt.Sprintf("spot-go@%s", config.Hostname)

	errConfigFileDoesNotExist = errors.New("config file does not exist")
)

type (
	credentials struct {
		Username string `json:"username"`
	}
	LoginResponse struct {
		Err error
	}
)

func init() {
	err := os.MkdirAll(ConfigFolder, os.ModePerm)
	if err != nil {
		panic(err)
	}

	// Ensure the process is always started by the spot-go.
	// This can change in the future, but for now it's a simple
	// solution to avoid issues.
	command.Run("pkill -9 spotifyd")
}

func LoginWithUserAndPassword(user, pass string) LoginResponse {
	errC := make(chan error)
	t := time.NewTimer(10 * time.Second)
	go func() {
		<-t.C
		errC <- errors.New("timeout trying to connect to spotify")
	}()

	c := fmt.Sprintf(
		"%s -u %s -p %s --no-daemon --cache-path %s --device-name %s --device-type computer",
		binaryFile,
		user,
		pass,
		ConfigFolder,
		deviceName,
	)
	go command.RunWithBufferedOutput(c, func(output string) {
		if strings.Contains(output, "failed to connect to spotify") {
			errC <- errors.New(output)
		}
		if strings.Contains(output, "Authenticated as") {
			errC <- nil
		}
	})
	err := <-errC
	t.Stop()

	return LoginResponse{err}
}

func LoginWithCredentials() LoginResponse {
	if !path.Exists(credentialsFile) {
		return LoginResponse{errConfigFileDoesNotExist}
	}

	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return LoginResponse{
			fmt.Errorf("fail to read file %s: %+v", credentialsFile, err),
		}
	}

	var credentials credentials
	err = json.Unmarshal(b, &credentials)
	if err != nil {
		return LoginResponse{
			fmt.Errorf("fail to unmarshal json: %+v", err),
		}
	}

	errC := make(chan error)
	t := time.NewTimer(10 * time.Second)
	go func() {
		<-t.C
		errC <- errors.New("timeout trying to connect to spotify")
	}()

	c := fmt.Sprintf(
		"%s -u %s --no-daemon --cache-path %s --device-name %s --device-type computer",
		binaryFile,
		credentials.Username,
		ConfigFolder,
		deviceName,
	)
	go command.RunWithBufferedOutput(c, func(output string) {
		if strings.Contains(output, "failed to connect to spotify") {
			errC <- errors.New(output)
		}
		if strings.Contains(output, "Authenticated as") {
			errC <- nil
		}
	})
	err = <-errC
	t.Stop()

	return LoginResponse{err}
}

func LoginDiscovery() LoginResponse {
	logged := make(chan bool)

	c := fmt.Sprintf(
		"%s --no-daemon --cache-path %s --device-name %s --device-type computer",
		binaryFile,
		ConfigFolder,
		deviceName,
	)
	go command.RunWithBufferedOutput(c, func(output string) {
		if strings.Contains(output, "Authenticated as") {
			logged <- true
		}
	})
	<-logged

	return LoginResponse{nil}
}

func IsBinaryValid() bool {
	return hash.SHA512Sum(binaryFile) == expectedBinaryFileSha512[runtime.GOOS]
}

func ShouldDownloadBinary() bool {
	return !path.Exists(binaryFile) || !IsBinaryValid()
}

func DeviceName() string {
	return deviceName
}
