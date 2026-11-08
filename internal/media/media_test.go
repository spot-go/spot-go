package media

import (
	"fmt"
	"os"
	"testing"
	"time"
)

type testRun struct {
	code     int
	finished time.Time
}

func useHandler(t *testing.T, fn func(Command)) {
	t.Helper()

	setHandler(fn)
	t.Cleanup(func() { setHandler(nil) })
}

func TestDispatchSendsTheCommandToTheHandler(t *testing.T) {
	got := make(chan Command, 1)
	useHandler(t, func(c Command) { got <- c })

	dispatch(NextTrack)

	select {
	case c := <-got:
		if c != NextTrack {
			t.Fatalf("command = %d, want %d", c, NextTrack)
		}
	case <-time.After(time.Second):
		t.Fatal("the handler was not called")
	}
}

func TestDispatchDoesNotWaitForTheHandler(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	useHandler(t, func(Command) { <-release })

	done := make(chan struct{})
	go func() {
		dispatch(TogglePlayPause)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch blocked on the handler")
	}
}

func TestDispatchWithoutAHandler(t *testing.T) {
	useHandler(t, nil)

	dispatch(TogglePlayPause)
}

func TestMain(m *testing.M) {
	done := make(chan testRun, 1)
	Main(func() {
		code := m.Run()
		done <- testRun{code: code, finished: time.Now()}
	})

	select {
	case run := <-done:
		if wait := time.Since(run.finished); wait > 500*time.Millisecond {
			fmt.Fprintf(os.Stderr, "Main returned %v after its function finished\n", wait)
			run.code = 1
		}
		os.Exit(run.code)
	default:
		fmt.Fprintln(os.Stderr, "Main returned before its function finished")
		os.Exit(1)
	}
}
