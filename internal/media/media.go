package media

import "time"

type (
	Command int

	Info struct {
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
