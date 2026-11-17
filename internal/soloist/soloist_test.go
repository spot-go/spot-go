package soloist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/command"
)

func TestAPIKey(t *testing.T) {
	useTempFolder(t)

	if HasAPIKey() {
		t.Fatal("HasAPIKey() = true before saving one")
	}
	if _, err := APIKey(); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("APIKey() = %v, want %v", err, ErrNoAPIKey)
	}
	if err := SaveAPIKey("   "); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("SaveAPIKey(blank) = %v, want %v", err, ErrNoAPIKey)
	}

	if err := os.WriteFile(apiKeyFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveAPIKey("  my-key\n"); err != nil {
		t.Fatalf("SaveAPIKey() = %v", err)
	}
	key, err := APIKey()
	if err != nil || key != "my-key" {
		t.Fatalf("APIKey() = %q, %v", key, err)
	}

	info, err := os.Stat(apiKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("api key file mode = %o, want 600", perm)
	}
}

func TestStalePatternMatchesDaemonArgs(t *testing.T) {
	useTempFolder(t)

	re := regexp.MustCompile(stalePattern(dataDir))
	cmdline := binaryFile + " " + strings.Join(daemonArgs("key"), " ")
	if !re.MatchString(cmdline) {
		t.Fatalf("stalePattern() does not match %q", cmdline)
	}

	other := strings.Replace(cmdline, dataDir, dataDir+"-other", 1)
	if re.MatchString(other) {
		t.Fatalf("stalePattern() matches another data dir: %q", other)
	}
}

func TestLoginWithStoredSession(t *testing.T) {
	useTempFolder(t)
	writeScript(t, binaryFile, fakeScript(time.Now(), `
echo "2026-11-07 16:55:43.837: ready, device_name=spot-go@host device_id=84680ce4-e21b-44ea-96eb-921b6f18fc63"
echo "2026-11-07 16:55:43.837: restoring session"
echo "2026-11-07 16:55:44.529: logged in as me"
exec sleep 60
`))
	if err := SaveAPIKey("key"); err != nil {
		t.Fatal(err)
	}

	var paired atomic.Bool
	resp := Login(func() { paired.Store(true) })
	if resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	if paired.Load() {
		t.Fatal("Login() asked to pair while restoring a session")
	}
	if id := DeviceID(); id != "84680ce4-e21b-44ea-96eb-921b6f18fc63" {
		t.Fatalf("DeviceID() = %q", id)
	}
	if err := Err(); err != nil {
		t.Fatalf("Err() after login = %v", err)
	}

	Stop()
	if err := Err(); !errors.Is(err, ErrStopped) {
		t.Fatalf("Err() after Stop() = %v, want %v", err, ErrStopped)
	}
}

func TestLoginPairing(t *testing.T) {
	useTempFolder(t)
	marker := filepath.Join(t.TempDir(), "paired")
	writeScript(t, binaryFile, fakeScript(time.Now(), `
echo "2026-11-07 16:55:52.825: ready, device_name=spot-go@host device_id=621d8765-e503-4833-bbac-3795c43ae65f"
echo '2026-11-07 16:55:52.825: waiting for login — connect to "spot-go@host" from your Spotify app'
while [ ! -f "`+marker+`" ]; do sleep 0.05; done
echo "2026-11-07 16:56:00.000: logged in as me"
exec sleep 60
`))
	if err := SaveAPIKey("key"); err != nil {
		t.Fatal(err)
	}

	var pairings atomic.Int32
	resp := Login(func() {
		pairings.Add(1)
		_ = os.WriteFile(marker, nil, 0644)
	})
	if resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	if n := pairings.Load(); n != 1 {
		t.Fatalf("onPairing was called %d times, want 1", n)
	}
}

func TestLoginErrors(t *testing.T) {
	tests := []struct {
		name   string
		daemon string
		want   error
		text   string
	}{
		{
			name: "invalid api key",
			daemon: `echo "2026-11-07 16:56:16.120: restoring session"
echo "2026-11-07 16:56:16.721: login failed: make sure --api-key is valid"
exec sleep 60`,
			want: ErrInvalidAPIKey,
		},
		{
			name: "expired build",
			daemon: `echo "2026-11-07 16:56:16.120: this build has expired" >&2
exit 10`,
			want: ErrExpired,
		},
		{
			name: "data dir in use",
			daemon: `echo 'Error: another session is running for data directory "/tmp/data".' >&2
echo "Stop the running session before starting soloist again." >&2
exit 1`,
			text: `Error: another session is running for data directory "/tmp/data".`,
		},
		{
			name: "crash",
			daemon: `echo "something went wrong" >&2
exit 3`,
			text: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTempFolder(t)
			writeScript(t, binaryFile, fakeScript(time.Now(), tt.daemon))
			if err := SaveAPIKey("key"); err != nil {
				t.Fatal(err)
			}

			resp := Login(nil)
			if tt.want != nil && !errors.Is(resp.Err, tt.want) {
				t.Fatalf("Login() = %v, want %v", resp.Err, tt.want)
			}
			if tt.text != "" && (resp.Err == nil || !strings.Contains(resp.Err.Error(), tt.text)) {
				t.Fatalf("Login() = %v, want an error with %q", resp.Err, tt.text)
			}
			if err := Err(); !errors.Is(err, ErrStopped) {
				t.Fatalf("Err() after a failed login = %v, want %v", err, ErrStopped)
			}
		})
	}
}

func TestLoginWithoutSetup(t *testing.T) {
	useTempFolder(t)

	if resp := Login(nil); !errors.Is(resp.Err, ErrNoAPIKey) {
		t.Fatalf("Login() without a key = %v, want %v", resp.Err, ErrNoAPIKey)
	}

	if err := SaveAPIKey("key"); err != nil {
		t.Fatal(err)
	}
	if resp := Login(nil); !errors.Is(resp.Err, ErrNotInstalled) {
		t.Fatalf("Login() without a binary = %v, want %v", resp.Err, ErrNotInstalled)
	}
}

func TestStopEndsPairing(t *testing.T) {
	useTempFolder(t)
	writeScript(t, binaryFile, fakeScript(time.Now(), `
echo '2026-11-07 16:55:52.825: waiting for login — connect to "spot-go@host" from your Spotify app'
trap 'exit 0' TERM
while true; do sleep 0.05; done
`))
	if err := SaveAPIKey("key"); err != nil {
		t.Fatal(err)
	}

	paired := make(chan struct{})
	done := make(chan LoginResponse, 1)
	go func() { done <- Login(func() { close(paired) }) }()

	select {
	case <-paired:
	case <-time.After(5 * time.Second):
		t.Fatal("Login() never asked to pair")
	}

	start := time.Now()
	Stop()
	select {
	case resp := <-done:
		if !errors.Is(resp.Err, ErrStopped) {
			t.Fatalf("Login() after Stop() = %v, want %v", resp.Err, ErrStopped)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Login() did not return after Stop()")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop() took %v, want SIGTERM to end the daemon right away", elapsed)
	}
}

// countingScript is a Soloist that logs in, and counts how many times it
// started in the file it returns.
func countingScript(t *testing.T, daemon string) string {
	t.Helper()

	starts := filepath.Join(t.TempDir(), "starts")
	writeScript(t, binaryFile, fakeScript(time.Now(), `echo started >> "`+starts+`"
`+daemon))
	if err := SaveAPIKey("key"); err != nil {
		t.Fatal(err)
	}
	return starts
}

// loggedInDaemon logs in and keeps running as itself, so the pattern that
// finds Soloist still matches it, as it would with an exec.
const loggedInDaemon = `echo "2026-11-07 16:55:43.837: ready, device_name=spot-go@host device_id=84680ce4-e21b-44ea-96eb-921b6f18fc63"
echo "2026-11-07 16:55:44.529: logged in as me"
while true; do sleep 1; done
`

func startCount(t *testing.T, starts string) int {
	t.Helper()

	b, err := os.ReadFile(starts)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "started")
}

func savedPid(t *testing.T) int {
	t.Helper()

	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func waitStopped(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for command.Running(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("Soloist %d still runs", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDetachLeavesSoloistPlayingForResume(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, loggedInDaemon)

	if resp := Login(nil); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	pid := savedPid(t)

	Detach()
	if !command.Running(pid) {
		t.Fatal("Detach() stopped Soloist")
	}
	if got, ok := LeftRunning(); !ok || got != pid {
		t.Fatalf("LeftRunning() = %d, %t after Detach(), want %d, true", got, ok, pid)
	}
	if !StillLeft(pid) {
		t.Fatal("StillLeft() = false after Detach()")
	}
	if Adopted() {
		t.Fatal("Adopted() = true with no Soloist in use")
	}
	if !LeftPlaying() {
		t.Fatal("LeftPlaying() = false after Detach()")
	}

	var paired atomic.Bool
	if resp := Resume(func() { paired.Store(true) }); resp.Err != nil {
		t.Fatalf("Resume() = %v", resp.Err)
	}
	if n := startCount(t, starts); n != 1 {
		t.Fatalf("Soloist started %d times, want Resume() to use the one left playing", n)
	}
	if paired.Load() {
		t.Fatal("Resume() asked to pair with a Soloist that logged in")
	}
	if !Adopted() {
		t.Fatal("Adopted() = false after Resume() found Soloist playing")
	}
	if id := DeviceID(); id != "84680ce4-e21b-44ea-96eb-921b6f18fc63" {
		t.Fatalf("DeviceID() = %q", id)
	}
	if err := Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	Stop()
	waitStopped(t, pid)
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the pid file is still there after Stop(): %v", err)
	}
	if LeftPlaying() {
		t.Fatal("LeftPlaying() = true after Stop()")
	}
	if StillLeft(pid) {
		t.Fatal("StillLeft() = true after Stop()")
	}
	if _, ok := LeftRunning(); ok {
		t.Fatal("LeftRunning() = true after Stop()")
	}
	if Adopted() {
		t.Fatal("Adopted() = true after Stop()")
	}
}

func TestResumeStartsSoloistWhenNoneWasLeftPlaying(t *testing.T) {
	tests := []struct {
		name string
		pid  func(t *testing.T) string
	}{
		{name: "no pid file"},
		{name: "exited", pid: func(t *testing.T) string {
			cmd := exec.Command("true")
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			return strconv.Itoa(cmd.Process.Pid)
		}},
		{name: "another program", pid: func(*testing.T) string { return strconv.Itoa(os.Getpid()) }},
		{name: "garbage", pid: func(*testing.T) string { return "soloist" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTempFolder(t)
			starts := countingScript(t, loggedInDaemon)
			// A log as a Soloist left playing has, so only the process ID
			// tells this isn't one.
			if err := os.WriteFile(logFile, []byte("2026-11-07 16:55:44.529: logged in as me\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if tt.pid != nil {
				if err := os.WriteFile(pidFile, []byte(tt.pid(t)+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if pid, ok := LeftRunning(); ok {
					t.Fatalf("LeftRunning() = %d, true for a pid file with %s", pid, tt.pid(t))
				}
			}

			if resp := Resume(nil); resp.Err != nil {
				t.Fatalf("Resume() = %v", resp.Err)
			}
			if n := startCount(t, starts); n != 1 {
				t.Fatalf("Soloist started %d times, want 1", n)
			}
			if Adopted() {
				t.Fatal("Adopted() = true for a Soloist this run started")
			}
			if pid := savedPid(t); !command.Running(pid) {
				t.Fatalf("the pid file has %d, which doesn't run", pid)
			}
		})
	}
}

func TestLoginStopsSoloistLeftPlaying(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, loggedInDaemon)

	if resp := Login(nil); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	old := savedPid(t)
	Detach()

	if resp := Login(nil); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	waitStopped(t, old)
	if n := startCount(t, starts); n != 2 {
		t.Fatalf("Soloist started %d times, want 2", n)
	}
	if pid := savedPid(t); pid == old || !command.Running(pid) {
		t.Fatalf("the pid file has %d, want the new Soloist", pid)
	}
}

func TestResumeRestartsSoloistLeftPlayingThatFailed(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, `if [ "$(wc -l < "$STARTS")" -eq 1 ]; then
  echo "2026-11-07 16:55:43.837: Error: the audio device went away"
  while true; do sleep 1; done
fi
`+loggedInDaemon)
	t.Setenv("STARTS", starts)

	p, err := start("key")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("the first Soloist never failed")
	}
	old := p.pid()
	Detach()

	if resp := Resume(nil); resp.Err != nil {
		t.Fatalf("Resume() = %v", resp.Err)
	}
	waitStopped(t, old)
	if n := startCount(t, starts); n != 2 {
		t.Fatalf("Soloist started %d times, want Resume() to start a new one", n)
	}
	if Adopted() {
		t.Fatal("Adopted() = true for the new Soloist")
	}
}

func TestResumeGoesOnFromWhereTheLoginGot(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, `echo "2026-11-07 16:55:52.825: ready, device_name=spot-go@host device_id=621d8765-e503-4833-bbac-3795c43ae65f"
echo '2026-11-07 16:55:52.825: waiting for login — connect to "spot-go@host" from your Spotify app'
sleep 0.2
echo "2026-11-07 16:56:00.000: logged in as me"
while true; do sleep 1; done
`)

	var pairings atomic.Int32
	if resp := Login(func() { pairings.Add(1) }); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	Detach()

	if resp := Resume(func() { pairings.Add(1) }); resp.Err != nil {
		t.Fatalf("Resume() = %v", resp.Err)
	}
	if n := pairings.Load(); n != 1 {
		t.Fatalf("asked to pair %d times, want only the first login to ask", n)
	}
	if n := startCount(t, starts); n != 1 {
		t.Fatalf("Soloist started %d times, want 1", n)
	}
	if id := DeviceID(); id != "621d8765-e503-4833-bbac-3795c43ae65f" {
		t.Fatalf("DeviceID() = %q", id)
	}
}

func TestResumeAsksToPairASoloistLeftWaitingForALogin(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, `echo '2026-11-07 16:55:52.825: waiting for login — connect to "spot-go@host" from your Spotify app'
while true; do sleep 1; done
`)

	p, err := start("key")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("Soloist never waited for a login")
	}
	pid := p.pid()
	Detach()

	paired := make(chan struct{})
	done := make(chan LoginResponse, 1)
	go func() { done <- Resume(func() { close(paired) }) }()
	select {
	case <-paired:
	case <-time.After(5 * time.Second):
		t.Fatal("Resume() never asked to pair")
	}

	// Quitting before the login ends leaves Soloist as spot-go found it.
	Detach()
	select {
	case resp := <-done:
		if !errors.Is(resp.Err, ErrStopped) {
			t.Fatalf("Resume() after Detach() = %v, want %v", resp.Err, ErrStopped)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resume() did not return after Detach()")
	}
	time.Sleep(200 * time.Millisecond)
	if !command.Running(pid) {
		t.Fatal("Soloist stopped after Detach()")
	}
	if n := startCount(t, starts); n != 1 {
		t.Fatalf("Soloist started %d times, want 1", n)
	}
}

func TestResumeRestartsSoloistLeftPlayingThatLoggedOut(t *testing.T) {
	useTempFolder(t)
	starts := countingScript(t, `if [ "$(wc -l < "$STARTS")" -eq 1 ]; then
  echo "2026-11-07 16:55:44.529: logged in as me"
  echo "2026-11-07 16:58:00.000: login failed: make sure --api-key is valid"
  while true; do sleep 1; done
fi
`+loggedInDaemon)
	t.Setenv("STARTS", starts)

	p, err := start("key")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("the first Soloist never failed")
	}
	old := p.pid()
	Detach()

	if resp := Resume(nil); resp.Err != nil {
		t.Fatalf("Resume() = %v", resp.Err)
	}
	waitStopped(t, old)
	if n := startCount(t, starts); n != 2 {
		t.Fatalf("Soloist started %d times, want Resume() to start a new one", n)
	}
}

func TestLeftRunningNeedsTheDataFolder(t *testing.T) {
	useTempFolder(t)
	countingScript(t, loggedInDaemon)

	if resp := Login(nil); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	pid := savedPid(t)
	if got, ok := LeftRunning(); !ok || got != pid {
		t.Fatalf("LeftRunning() = %d, %t, want %d, true", got, ok, pid)
	}

	other := dataDir
	dataDir += "-other"
	defer func() { dataDir = other }()
	if got, ok := LeftRunning(); ok {
		t.Fatalf("LeftRunning() = %d, true for a Soloist on another data folder", got)
	}
}

func TestStillLeftFollowsTheSoloistLeftPlaying(t *testing.T) {
	useTempFolder(t)
	countingScript(t, loggedInDaemon)
	if resp := Login(nil); resp.Err != nil {
		t.Fatalf("Login() = %v", resp.Err)
	}
	pid := savedPid(t)
	Detach()

	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = other.Process.Kill()
		_ = other.Wait()
	})
	if StillLeft(other.Process.Pid) {
		t.Fatal("StillLeft() = true for a process the pid file doesn't name")
	}
	if !StillLeft(pid) {
		t.Fatal("StillLeft() = false for the Soloist left playing")
	}

	// As when Soloist crashes, or the user ends it: the pid file stays.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	waitStopped(t, pid)
	if StillLeft(pid) {
		t.Fatal("StillLeft() = true once the Soloist left playing ended")
	}
}
