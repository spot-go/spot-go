package command

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "fake spotifyd")
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

	var mu sync.Mutex
	var lines []string
	p, err := Start(script, []string{"'user with space'"}, func(line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = p.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("Wait() = %v, want exit status 3", err)
	}

	sort.Strings(lines)
	want := []string{
		"[INFO] Authenticated as 'user with space' !",
		"partial line without newline",
		"stderr line",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", lines, want)
		}
	}
}

func TestStartMissingBinary(t *testing.T) {
	if _, err := Start(filepath.Join(t.TempDir(), "missing"), nil, nil); err == nil {
		t.Fatal("Start() with a missing binary returned no error")
	}
}

func TestKill(t *testing.T) {
	script := writeScript(t, "echo started\nexec sleep 60\n")

	started := make(chan struct{})
	var once sync.Once
	p, err := Start(script, nil, func(string) {
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

func TestKillProcessGroup(t *testing.T) {
	script := writeScript(t, "echo started\nsleep 60\necho never\n")

	started := make(chan struct{})
	var once sync.Once
	p, err := Start(script, nil, func(string) {
		once.Do(func() { close(started) })
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started

	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Wait() after Kill() returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() blocked: a child process kept the output pipe open after Kill()")
	}
}
