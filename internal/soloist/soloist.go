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
	"syscall"
	"time"

	"github.com/spot-go/spot-go/internal/command"
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
	// logFile has what Soloist writes. It goes to a file and not to spot-go,
	// so Soloist can keep playing after spot-go quits.
	logFile = filepath.Join(Folder, "soloist.log")
	// pidFile has the process ID of the Soloist spot-go started last, so the
	// next run of spot-go can find it when it was left playing.
	pidFile = filepath.Join(Folder, "soloist.pid")

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

// Login starts Soloist and waits for it to log in. It first stops any
// Soloist that runs with spot-go's data folder, such as one an earlier run of
// spot-go left playing.
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
			stopProcess(p)
		}
		return LoginResponse{err}
	}
	return LoginResponse{nil}
}

// Resume uses the Soloist an earlier run of spot-go left playing, when there
// is one, so what it plays goes on. Otherwise, or when that Soloist doesn't
// log in, it starts Soloist like Login.
func Resume(onPairing func()) LoginResponse {
	if _, err := APIKey(); err != nil {
		return LoginResponse{err}
	}

	if p := adopt(); p != nil {
		err := p.waitLoggedIn(loginTimeout, onPairing)
		if err == nil || errors.Is(err, ErrStopped) {
			return LoginResponse{err}
		}
		if !release(p) {
			// spot-go stopped or left it meanwhile, so it isn't the one to
			// start another.
			return LoginResponse{ErrStopped}
		}
		stopProcess(p)
	}
	return Login(onPairing)
}

// LeftPlaying reports whether a Soloist an earlier run of spot-go left
// playing still runs.
func LeftPlaying() bool {
	_, ok := leftRunning()
	return ok
}

// adopt follows the Soloist an earlier run of spot-go left playing, if it
// still runs.
func adopt() *process {
	startMu.Lock()
	defer startMu.Unlock()

	mu.Lock()
	running := current != nil
	mu.Unlock()
	if running {
		return nil
	}

	pid, ok := leftRunning()
	if !ok {
		return nil
	}
	p, err := attachProcess(pid, logFile)
	if err != nil {
		return nil
	}

	mu.Lock()
	current = p
	mu.Unlock()
	return p
}

// leftRunning is the process ID of the Soloist spot-go started last, when it
// still runs with spot-go's data folder.
func leftRunning() (int, bool) {
	pid, ok := pidInFile()
	if !ok || !command.Running(pid) {
		return 0, false
	}

	// The process ID may have gone to another program since.
	line, ok := command.Line(pid)
	if !ok || !regexp.MustCompile(stalePattern(dataDir)).MatchString(line) {
		return 0, false
	}
	return pid, true
}

// StillLeft reports whether pid, a Soloist Detach left playing, still runs,
// and no run of spot-go started another one since.
func StillLeft(pid int) bool {
	saved, ok := pidInFile()
	return ok && saved == pid && command.Running(pid)
}

// pidInFile is the process ID of the Soloist spot-go started last.
func pidInFile() (int, bool) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil
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

// Stop stops Soloist, and with it what it plays.
func Stop() {
	startMu.Lock()
	defer startMu.Unlock()

	stopCurrent()
}

// Detach leaves Soloist running when spot-go quits, so what it plays keeps
// playing. The next run of spot-go finds it with Resume. Detach returns the
// process ID of the Soloist it left running, or 0 when none runs.
func Detach() int {
	startMu.Lock()
	defer startMu.Unlock()

	mu.Lock()
	p := current
	current = nil
	mu.Unlock()

	if p == nil {
		return 0
	}
	p.detach()
	if pid := p.pid(); command.Running(pid) {
		return pid
	}
	return 0
}

// Adopted reports whether the Soloist in use is one an earlier run of
// spot-go left playing.
func Adopted() bool {
	mu.Lock()
	defer mu.Unlock()

	return current != nil && current.adopted
}

// killStale stops every Soloist that runs with spot-go's data folder: one an
// earlier run of spot-go left playing, or one left behind when spot-go
// crashed.
func killStale() {
	pattern := stalePattern(dataDir)
	// The Soloist spot-go started last is found without pkill and pgrep,
	// which some Linux systems don't have.
	left, hasLeft := leftRunning()
	stopped := func() bool {
		if hasLeft && command.Running(left) {
			return false
		}
		return exec.Command("pgrep", "-f", pattern).Run() != nil
	}

	if hasLeft {
		_ = syscall.Kill(-left, syscall.SIGTERM)
	}
	_ = exec.Command("pkill", "-f", pattern).Run()

	deadline := time.Now().Add(staleStopTimeout)
	for time.Now().Before(deadline) {
		if stopped() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hasLeft {
		_ = syscall.Kill(-left, syscall.SIGKILL)
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
	killStale()
	_ = os.Remove(pidFile)
	p, err := startProcess(binaryFile, daemonArgs(key), logFile)
	if err != nil {
		return nil, err
	}
	// Without the file, the next run of spot-go can't find this Soloist if
	// it is left playing, and stops it instead.
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(p.pid())+"\n"), 0600)

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
		stopProcess(p)
	}
}

// stopProcess stops p, and forgets it was started.
func stopProcess(p *process) {
	p.stop()
	if b, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(b)) == strconv.Itoa(p.pid()) {
		_ = os.Remove(pidFile)
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
