package process

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeSpotifyd(t *testing.T, body string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "spotifyd")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func start(t *testing.T, body string, onLine func(string)) *Process {
	t.Helper()

	p, err := Start(fakeSpotifyd(t, body), nil, onLine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Stop)
	return p
}

func TestWaitAuthenticated(t *testing.T) {
	p := start(t, `printf '[\033[32mINFO\033[0m] Authenticated as '"'"'me'"'"' !\n'
exec sleep 60
`, nil)

	if err := p.WaitAuthenticated(5 * time.Second); err != nil {
		t.Fatalf("WaitAuthenticated() = %v", err)
	}
	if err := p.Err(); err != nil {
		t.Fatalf("Err() after login = %v", err)
	}
}

func TestWaitAuthenticatedFailureLine(t *testing.T) {
	p := start(t, `echo "[INFO] Failed reading config file"
printf 'Error: \n   0: Discovery unavailable and no credentials found.\n' >&2
exec sleep 60
`, nil)

	err := p.WaitAuthenticated(5 * time.Second)
	if err == nil || err.Error() != "0: Discovery unavailable and no credentials found." {
		t.Fatalf("WaitAuthenticated() = %v, want the error message after the Error: header", err)
	}
}

func TestWaitAuthenticatedAfterRetry(t *testing.T) {
	p := start(t, `echo "[ERROR] connection to spotify failed: Service unavailable"
echo "[INFO] retrying connection in 5 seconds (retry 1/4)"
sleep 0.3
echo "[INFO] Authenticated as 'me' !"
exec sleep 60
`, nil)

	if err := p.WaitAuthenticated(5 * time.Second); err != nil {
		t.Fatalf("WaitAuthenticated() = %v, want login after a retry", err)
	}
	if err := p.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestWaitAuthenticatedExitWithErrorReport(t *testing.T) {
	p := start(t, `echo "[INFO] Authenticated as 'me' !"
printf 'Error: \n   0: DBus terminated unexpectedly\n   1: Failed to connect to session bus\n' >&2
exit 1
`, nil)

	if err := p.WaitAuthenticated(5 * time.Second); err != nil {
		t.Fatalf("WaitAuthenticated() = %v", err)
	}
	<-p.exited
	err := p.Err()
	if err == nil || !strings.Contains(err.Error(), "DBus terminated unexpectedly") {
		t.Fatalf("Err() = %v, want the full error report", err)
	}
}

func TestWaitAuthenticatedExit(t *testing.T) {
	p := start(t, `echo "something went wrong" >&2
exit 1
`, nil)

	err := p.WaitAuthenticated(5 * time.Second)
	if err == nil || !strings.Contains(err.Error(), "something went wrong") {
		t.Fatalf("WaitAuthenticated() = %v, want the exit error with output", err)
	}
}

func TestWaitAuthenticatedTimeout(t *testing.T) {
	p := start(t, "echo waiting\nexec sleep 60\n", nil)

	err := p.WaitAuthenticated(200 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "waiting") {
		t.Fatalf("WaitAuthenticated() = %v, want a timeout with output", err)
	}
}

func TestStop(t *testing.T) {
	p := start(t, "echo waiting\nexec sleep 60\n", nil)

	done := make(chan error, 1)
	go func() { done <- p.WaitAuthenticated(0) }()
	p.Stop()

	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("WaitAuthenticated() after Stop() = %v, want ErrStopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitAuthenticated() did not return after Stop()")
	}
	if err := p.Wait(); !errors.Is(err, ErrStopped) {
		t.Fatalf("Wait() after Stop() = %v, want ErrStopped", err)
	}
}

func TestErrAfterLogin(t *testing.T) {
	p := start(t, `echo "Authenticated as 'me' !"
sleep 0.2
echo "[ERROR] librespot does not support \"free\" accounts."
exec sleep 60
`, nil)

	if err := p.WaitAuthenticated(5 * time.Second); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for p.Err() == nil {
		if time.Now().After(deadline) {
			t.Fatal("Err() never reported the failure logged after login")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(p.Err().Error(), "free") {
		t.Fatalf("Err() = %v", p.Err())
	}
}

func TestRepeatedEventsDoNotBlock(t *testing.T) {
	p := start(t, `i=0
while [ $i -lt 200 ]; do
  echo "Authenticated as 'me' !"
  echo "[ERROR] connection to spotify failed: retry"
  i=$((i+1))
done
echo finished
`, nil)

	if err := p.WaitAuthenticated(5 * time.Second); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process output blocked after repeated events")
	}
}

func TestOnLine(t *testing.T) {
	var mu sync.Mutex
	var urls []string
	p := start(t, `echo "Browse to: https://accounts.spotify.com/authorize?x=1"
exit 0
`, func(line string) {
		if url, ok := AuthURL(line); ok {
			mu.Lock()
			urls = append(urls, url)
			mu.Unlock()
		}
	})

	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(urls) != 1 || urls[0] != "https://accounts.spotify.com/authorize?x=1" {
		t.Fatalf("urls = %q", urls)
	}
}
