package spotifyd

import (
	"errors"
	"fmt"
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
	spotifydFile           = filepath.Join(config.Path, "spotifyd")
	spotifydExpectedSha512 = map[string]string{
		"linux":  "1cc4897e2394f37273e030b8633a7f6fba6fe232d560f10be03b3a74eba2c001704d40864501b68917b4c24691abf01f7e529c0673167e509467eabe3bab524d",
		"darwin": "eb189206192894f5e11615a0154efaec5226297181f5e2f2fec58b8f2fb47eacd84ba00acb752ca153ada4e81f67bb7ab2adf893e756f3ffda067667e528a77b",
	}
)

func LoginWithUserAndPassword(user, pass string) (err error) {
	errC := make(chan error)
	t := time.NewTimer(10 * time.Second)
	go func() {
		<-t.C
		errC <- errors.New("timeout trying to connect to spotify")
	}()

	c := fmt.Sprintf(
		"%s -u %s -p %s --no-daemon -c %s",
		spotifydFile,
		user,
		pass,
		config.Path,
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

	return err
}

func IsBinaryValid() bool {
	return hash.SHA512Sum(spotifydFile) == spotifydExpectedSha512[runtime.GOOS]
}

func ShouldDownloadBinary() bool {
	return !path.Exists(spotifydFile) || !IsBinaryValid()
}
