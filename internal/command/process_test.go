package command

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestMain also runs as a stand-in for spot-go that starts a process and
// exits right away, for TestStartedProcessOutlivesSpotGo.
func TestMain(m *testing.M) {
	if script := os.Getenv("SPOT_GO_TEST_START"); script != "" {
		p, err := Start(script, nil, os.Getenv("SPOT_GO_TEST_LOG"), nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(p.Pid())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func logPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "player.log")
}

// lines collects the lines a process writes.
type lines struct {
	mu   sync.Mutex
	got  []string
	seen chan string
}

func newLines() *lines {
	return &lines{seen: make(chan string, 100)}
}

func (l *lines) add(line string) {
	l.mu.Lock()
	l.got = append(l.got, line)
	l.mu.Unlock()
	l.seen <- line
}

func (l *lines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func (l *lines) waitFor(t *testing.T, want string) {
	t.Helper()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case line := <-l.seen:
			if line == want {
				return
			}
		case <-timeout:
			t.Fatalf("never got the line %q, got %q", want, l.all())
		}
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "fake player")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestStart(t *testing.T) {
	script := writeScript(t, `printf '[\033[32mINFO\033[0m] Authenticated as %s !\n' "$1"
printf 'stderr line\r\n' >&2
printf '\n'
printf 'partial line without newline'
exit 3
`)

	log := logPath(t)
	if err := os.WriteFile(log, []byte("from the last run\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := newLines()
	p, err := Start(script, []string{"'user with space'"}, log, got.add)
	if err != nil {
		t.Fatal(err)
	}

	err = p.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("Wait() = %v, want exit status 3", err)
	}

	want := []string{
		"[INFO] Authenticated as 'user with space' !",
		"stderr line",
		"partial line without newline",
	}
	if lines := got.all(); strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "from the last run") || !strings.Contains(string(b), "Authenticated as") {
		t.Fatalf("log = %q, want only the output of this run", b)
	}
}

func TestStartMissingBinary(t *testing.T) {
	if _, err := Start(filepath.Join(t.TempDir(), "missing"), nil, logPath(t), nil); err == nil {
		t.Fatal("Start() with a missing binary returned no error")
	}
}

func TestStartWithRelativePaths(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("player", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("player", "run"), []byte("#!/bin/sh\necho \"$1\"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	got := newLines()
	p, err := Start(filepath.Join("player", "run"), []string{"hello"}, filepath.Join("player", "run.log"), got.add)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if lines := got.all(); len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("lines = %q, want [hello]", lines)
	}
}

func TestStartRunsInItsOwnSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads the session from /proc")
	}
	script := writeScript(t, "echo started\nexec sleep 60\n")

	got := newLines()
	p, err := Start(script, nil, logPath(t), got.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = p.Kill()
		_ = p.Wait()
	})
	got.waitFor(t, "started")

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", p.Pid()))
	if err != nil {
		t.Fatal(err)
	}
	// The fields after the command name are the state, the parent, the
	// process group and the session.
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if session := fields[3]; session != strconv.Itoa(p.Pid()) {
		t.Fatalf("session = %s, want the process to lead its own session %d", session, p.Pid())
	}
}

// TestStartedProcessOutlivesSpotGo checks that a process keeps running, and
// writing its output, after the spot-go that started it exits, as Soloist
// does when the music keeps playing after spot-go quits. Through a pipe to
// spot-go, its first line after that would kill it.
func TestStartedProcessOutlivesSpotGo(t *testing.T) {
	script := writeScript(t, "while true; do echo tick; sleep 0.05; done\n")
	log := logPath(t)

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SPOT_GO_TEST_START="+script, "SPOT_GO_TEST_LOG="+log)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("spot-go stand-in failed: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("spot-go stand-in printed %q", out)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	size := func() int64 {
		info, err := os.Stat(log)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	before := size()
	time.Sleep(500 * time.Millisecond)
	if !Running(pid) {
		t.Fatal("the process stopped when spot-go exited")
	}
	if after := size(); after <= before {
		t.Fatalf("the log stayed at %d bytes after spot-go exited, want the process to keep writing", after)
	}
}

func TestAttach(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "go on")
	script := writeScript(t, `echo first
while [ ! -f "`+marker+`" ]; do sleep 0.05; done
echo second
trap 'echo stopping; exit 0' TERM
while true; do sleep 0.05; done
`)
	log := logPath(t)

	started := newLines()
	p, err := Start(script, nil, log, started.add)
	if err != nil {
		t.Fatal(err)
	}
	// As the process is a child of the test, its first Process has to reap
	// it once it exits, as Soloist's does in spot-go.
	go func() { _ = p.Wait() }()
	t.Cleanup(func() { _ = p.Kill() })
	started.waitFor(t, "first")
	p.Release()

	past, attached := newLines(), newLines()
	a, err := Attach(p.Pid(), log, past.add, attached.add)
	if err != nil {
		t.Fatal(err)
	}
	// The lines already in the log come before Attach returns.
	if lines := past.all(); len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("past lines = %q, want [first]", lines)
	}

	if err := os.WriteFile(marker, nil, 0644); err != nil {
		t.Fatal(err)
	}
	attached.waitFor(t, "second")
	if lines := started.all(); len(lines) != 1 {
		t.Fatalf("the released Process still got lines: %q", lines)
	}

	if err := a.Terminate(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() on an attached process did not return after it exited")
	}
	if Running(p.Pid()) {
		t.Fatal("the process still runs after Terminate()")
	}
	if lines := attached.all(); lines[len(lines)-1] != "stopping" {
		t.Fatalf("lines = %q, want the last line before it exited", lines)
	}
}

func TestAttachNotRunning(t *testing.T) {
	script := writeScript(t, "exit 0\n")
	p, err := Start(script, nil, logPath(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Wait()

	if _, err := Attach(p.Pid(), logPath(t), nil, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Attach() to an exited process = %v, want %v", err, ErrNotRunning)
	}
}

func TestReleasedAttachedProcessStopsWaiting(t *testing.T) {
	script := writeScript(t, "echo started\nexec sleep 60\n")
	log := logPath(t)

	got := newLines()
	p, err := Start(script, nil, log, got.add)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.Wait() }()
	t.Cleanup(func() { _ = p.Kill() })
	got.waitFor(t, "started")

	a, err := Attach(p.Pid(), log, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Wait() }()
	a.Release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() kept waiting for a released process")
	}
	if !Running(p.Pid()) {
		t.Fatal("Release() stopped the process")
	}
}

func TestKill(t *testing.T) {
	script := writeScript(t, "echo started\nexec sleep 60\n")

	started := make(chan struct{})
	var once sync.Once
	p, err := Start(script, nil, logPath(t), func(string) {
		once.Do(func() { close(started) })
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started

	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait() after Kill() returned no error")
	}
}

func TestSignalsReachTheProcessGroup(t *testing.T) {
	signals := map[string]func(*Process) error{
		"Kill":      (*Process).Kill,
		"Terminate": (*Process).Terminate,
	}
	for name, signal := range signals {
		t.Run(name, func(t *testing.T) {
			script := writeScript(t, "sleep 60 &\necho \"child $!\"\nwait\necho never\n")

			children := make(chan int, 1)
			p, err := Start(script, nil, logPath(t), func(line string) {
				if pid, err := strconv.Atoi(strings.TrimPrefix(line, "child ")); err == nil {
					children <- pid
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			var child int
			select {
			case child = <-children:
			case <-time.After(5 * time.Second):
				t.Fatal("the script never started its child")
			}
			t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

			if err := signal(p); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- p.Wait() }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("Wait() after %s() returned no error", name)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("Wait() blocked after %s()", name)
			}

			deadline := time.Now().Add(5 * time.Second)
			for !gone(child) {
				if time.Now().After(deadline) {
					t.Fatalf("%s() left the child of the process running", name)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

// gone reports whether pid exited, even if nothing reaped it yet.
func gone(pid int) bool {
	if !Running(pid) {
		return true
	}
	if runtime.GOOS == "linux" {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return true
		}
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		return len(fields) > 0 && fields[0] == "Z"
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err != nil || strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
