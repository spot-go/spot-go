package media

import (
	"errors"
	"sync"
	"time"
)

type (
	Command int

	Info struct {
		ID     string
		Title  string
		Artist string
		// Artwork is the URL of the cover of what plays, such as its
		// album's, or empty when there is none.
		Artwork  string
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

// later is info d after it was published: what plays moves on, up to its
// end, while it plays.
func (info Info) later(d time.Duration) Info {
	if info.Playing {
		info.Elapsed += d
		if info.Duration > 0 {
			info.Elapsed = min(info.Elapsed, info.Duration)
		}
	}
	return info
}
