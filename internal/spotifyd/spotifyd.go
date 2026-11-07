package spotifyd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/path"
	"github.com/spot-go/spot-go/internal/spotifyd/process"
)

const (
	loginTimeout   = 30 * time.Second
	versionTimeout = 10 * time.Second
)

var (
	ConfigFolder        = filepath.Join(config.Path, "spotifyd")
	configFile          = filepath.Join(ConfigFolder, "spotifyd.conf")
	oauthCredentials    = filepath.Join(ConfigFolder, "oauth", "credentials.json")
	zeroconfCredentials = filepath.Join(ConfigFolder, "zeroconf", "credentials.json")

	ErrCredentialsNotFound = errors.New("spotifyd credentials not found")
	ErrStopped             = process.ErrStopped
	ErrNoAudioKey          = errors.New("spotifyd got no audio key from Spotify")

	playbackErrors = make(chan error, 1)

	startMu sync.Mutex
	mu      sync.Mutex
	binary  string
	current *process.Process
)

type (
	LoginResponse struct {
		Err error
	}
	AuthenticateResponse struct {
		Err error
	}
)

func CreateConfigFolder() error {
	err := os.MkdirAll(ConfigFolder, os.ModePerm)
	if err != nil {
		return fmt.Errorf("fail to create spotifyd config dir: %w", err)
	}
	return nil
}

func KillStale() {
	_ = exec.Command("pkill", "-f", process.StalePattern(ConfigFolder)).Run()
}

func Stop() {
	startMu.Lock()
	defer startMu.Unlock()

	stopCurrent()
}

func Err() error {
	mu.Lock()
	p := current
	mu.Unlock()

	if p == nil {
		return ErrStopped
	}
	return p.Err()
}

func Authenticate(onURL func(url string)) AuthenticateResponse {
	port, err := freePort()
	if err != nil {
		return AuthenticateResponse{fmt.Errorf("fail to find a free port: %w", err)}
	}

	p, err := start([]string{
		"authenticate",
		"--cache-path", ConfigFolder,
		"--config-path", configFile,
		"--oauth-port", strconv.Itoa(port),
	}, func(line string) {
		if url, ok := process.AuthURL(line); ok && onURL != nil {
			onURL(url)
		}
	})
	if err != nil {
		return AuthenticateResponse{err}
	}

	err = p.Wait()
	release(p)
	if err != nil {
		return AuthenticateResponse{err}
	}
	if !path.Exists(oauthCredentials) {
		return AuthenticateResponse{errors.New("spotifyd finished without saving credentials")}
	}
	return AuthenticateResponse{nil}
}

func LoginWithCredentials() LoginResponse {
	if !path.Exists(oauthCredentials) && !path.Exists(zeroconfCredentials) {
		return LoginResponse{ErrCredentialsNotFound}
	}

	return runDaemon(loginTimeout, "--disable-discovery")
}

func LoginDiscovery() LoginResponse {
	return runDaemon(0)
}

func DeviceName() string {
	return fmt.Sprintf("spot-go@%s", config.Hostname)
}

func runDaemon(timeout time.Duration, extraArgs ...string) LoginResponse {
	args := []string{
		"--no-daemon",
		"--cache-path", ConfigFolder,
		"--config-path", configFile,
		"--device-name", DeviceName(),
		"--device-type", "computer",
		"--no-audio-cache",
	}
	args = append(args, platformDaemonArgs...)
	args = append(args, extraArgs...)

	p, err := start(args, reportPlaybackError)
	if err != nil {
		return LoginResponse{err}
	}

	if err := p.WaitAuthenticated(timeout); err != nil {
		if release(p) {
			p.Stop()
		}
		return LoginResponse{err}
	}
	return LoginResponse{nil}
}

func PlaybackErrors() <-chan error {
	return playbackErrors
}

func reportPlaybackError(line string) {
	if process.Classify(line) != process.AudioKeyRefused {
		return
	}
	select {
	case playbackErrors <- ErrNoAudioKey:
	default:
	}
}

func start(args []string, onLine func(string)) (*process.Process, error) {
	bin, err := resolveBinary()
	if err != nil {
		return nil, err
	}

	startMu.Lock()
	defer startMu.Unlock()

	stopCurrent()
	p, err := process.Start(bin, args, onLine)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	current = p
	mu.Unlock()
	return p, nil
}

func stopCurrent() {
	mu.Lock()
	p := current
	current = nil
	mu.Unlock()

	if p != nil {
		p.Stop()
	}
}

func release(p *process.Process) bool {
	mu.Lock()
	defer mu.Unlock()

	if current != p {
		return false
	}
	current = nil
	return true
}

func resolveBinary() (string, error) {
	mu.Lock()
	bin := binary
	mu.Unlock()

	if bin != "" {
		return bin, nil
	}
	if err := CheckBinary(); err != nil {
		return "", err
	}

	mu.Lock()
	defer mu.Unlock()
	return binary, nil
}

func setBinary(bin string) {
	mu.Lock()
	binary = bin
	mu.Unlock()
}

func binaryVersion(bin string) (process.Version, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return process.Version{}, fmt.Errorf("fail to run %s --version: %v\n%s", bin, err, text)
	}

	v, ok := process.ParseVersion(text)
	if !ok {
		return process.Version{}, fmt.Errorf("unexpected output from %s --version: %s", bin, text)
	}
	return v, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}
