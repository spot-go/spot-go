package media

import (
	"errors"
	"sync"
	"time"
)

type (
	Command int

	Info struct {
		ID       string
		Title    string
		Artist   string
		Duration time.Duration
		Elapsed  time.Duration
		Playing  bool
	}
)

const (
	TogglePlayPause Command = iota
	Play
	Pause
	NextTrack
	PreviousTrack
)

var (
	// ErrUnsupported is why spot-go has no media controls, on a system or
	// a build where it can't reach them.
	ErrUnsupported = errors.New("spot-go has no media controls here")

	handlerMu sync.Mutex
	handler   func(Command)
)

func setHandler(fn func(Command)) {
	handlerMu.Lock()
	handler = fn
	handlerMu.Unlock()
}

func dispatch(c Command) {
	handlerMu.Lock()
	fn := handler
	handlerMu.Unlock()
	if fn != nil {
		go fn(c)
	}
}
