//go:build darwin && cgo

package media

import (
	"testing"
	"time"
)

func setHandler(t *testing.T, fn func(Command)) {
	t.Helper()

	handlerMu.Lock()
	handler = fn
	handlerMu.Unlock()
	t.Cleanup(func() {
		handlerMu.Lock()
		handler = nil
		handlerMu.Unlock()
	})
}

func TestDispatchSendsTheCommandToTheHandler(t *testing.T) {
	got := make(chan Command, 1)
	setHandler(t, func(c Command) { got <- c })

	dispatch(int(NextTrack))

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
	setHandler(t, func(Command) { <-release })

	done := make(chan struct{})
	go func() {
		dispatch(int(TogglePlayPause))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch blocked on the handler, which would block the macOS main thread")
	}
}

func TestDispatchWithoutAHandler(t *testing.T) {
	setHandler(t, nil)

	dispatch(int(TogglePlayPause))
}
