package media

import (
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
