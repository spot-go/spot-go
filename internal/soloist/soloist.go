package soloist

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/config"
)

const (
	APIKeyPage = "https://developer.spotify.com/dashboard/soloist"

	loginTimeout     = 30 * time.Second
	staleStopTimeout = 5 * time.Second
	cacheSizeMB      = 512
)

var (
	Folder     = filepath.Join(config.Path, "soloist")
	binaryFile = filepath.Join(Folder, "soloist")
	apiKeyFile = filepath.Join(Folder, "api-key")
	volumeFile = filepath.Join(Folder, "volume")
	dataDir    = filepath.Join(Folder, "data")

	ErrNoAPIKey      = errors.New("no Soloist API key")
	ErrInvalidAPIKey = errors.New("Spotify rejected the Soloist API key")
	ErrStopped       = errors.New("Soloist was stopped")

	startMu sync.Mutex
	mu      sync.Mutex
	current *process
)

type LoginResponse struct {
	Err error
}

func CreateFolder() error {
	if err := os.MkdirAll(Folder, 0700); err != nil {
		return fmt.Errorf("fail to create the Soloist dir: %w", err)
	}
	return nil
}

func DeviceName() string {
	return fmt.Sprintf("spot-go@%s", config.Hostname)
}

func HasAPIKey() bool {
	_, err := APIKey()
	return err == nil
}

func APIKey() (string, error) {
	b, err := os.ReadFile(apiKeyFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoAPIKey
	}
	if err != nil {
		return "", fmt.Errorf("fail to read the Soloist API key: %w", err)
	}

	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", ErrNoAPIKey
	}
	return key, nil
}

func SaveAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrNoAPIKey
	}

	if err := os.WriteFile(apiKeyFile, []byte(key+"\n"), 0600); err != nil {
		return fmt.Errorf("fail to save the Soloist API key: %w", err)
	}
	if err := os.Chmod(apiKeyFile, 0600); err != nil {
		return fmt.Errorf("fail to protect the Soloist API key: %w", err)
	}
	return nil
}

func Login(onPairing func()) LoginResponse {
	key, err := APIKey()
	if err != nil {
		return LoginResponse{err}
	}
	if _, err := CheckBinary(); err != nil {
		return LoginResponse{err}
	}

	p, err := start(key)
	if err != nil {
		return LoginResponse{err}
	}

	if err := p.waitLoggedIn(loginTimeout, onPairing); err != nil {
		if release(p) {
			p.stop()
		}
		return LoginResponse{err}
	}
	return LoginResponse{nil}
}

func DeviceID() string {
	mu.Lock()
	p := current
	mu.Unlock()

	if p != nil {
		if id := p.deviceID(); id != "" {
			return id
		}
	}

	b, err := os.ReadFile(filepath.Join(dataDir, ".device_id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func Err() error {
	mu.Lock()
	p := current
	mu.Unlock()

	if p == nil {
		return ErrStopped
	}
	return p.err()
}

func Stop() {
	startMu.Lock()
	defer startMu.Unlock()

	stopCurrent()
}

func KillStale() {
	pattern := stalePattern(dataDir)
	_ = exec.Command("pkill", "-f", pattern).Run()

	deadline := time.Now().Add(staleStopTimeout)
	for time.Now().Before(deadline) {
		if exec.Command("pgrep", "-f", pattern).Run() != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-f", pattern).Run()
}

func stalePattern(dir string) string {
	return "soloist .*--data-dir " + regexp.QuoteMeta(dir) + "( |$)"
}

func daemonArgs(key string) []string {
	args := []string{
		"--device-name", DeviceName(),
		"--api-key", key,
		"--data-dir", dataDir,
		"--cache-dir", cacheDir(),
		"--cache-size", strconv.Itoa(cacheSizeMB),
		"--ws", wsAddr,
	}
	if percent, ok := savedVolume(); ok {
		args = append(args, "--initial-volume", strconv.Itoa(percent))
	}
	return args
}

func cacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "spot-go", "soloist")
	}
	return filepath.Join(Folder, "cache")
}

func start(key string) (*process, error) {
	startMu.Lock()
	defer startMu.Unlock()

	stopCurrent()
	p, err := startProcess(binaryFile, daemonArgs(key))
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
		p.stop()
	}
}

func release(p *process) bool {
	mu.Lock()
	defer mu.Unlock()

	if current != p {
		return false
	}
	current = nil
	return true
}
