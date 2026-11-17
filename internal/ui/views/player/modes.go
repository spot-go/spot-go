package player

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	// playerMode is shuffle or repeat.
	playerMode int

	// modeControl changes a mode. Presses while a change runs add up, and
	// run once it ends. device and plays are where Spotify played, and what
	// plays was, at the last change.
	modeControl struct {
		busy    bool
		pending int
		at      time.Time
		device  libspotify.ID
		plays   int
	}

	modeChanged struct {
		mode    playerMode
		device  libspotify.ID
		plays   int
		shuffle bool
		repeat  spotify.Repeat
		err     error
	}
)

const (
	shuffleMode playerMode = iota
	repeatMode
	modeCount
)

// repeatCycle is the order the repeat key goes through, as in the Spotify
// app.
var repeatCycle = []spotify.Repeat{spotify.RepeatOff, spotify.RepeatContext, spotify.RepeatTrack}

// changing reports whether spot-go is changing the mode, or changed it a
// moment ago, so what Spotify reports may not show the change yet.
func (c modeControl) changing() bool {
	return c.busy || !c.at.IsZero() && time.Since(c.at) < followGrace
}

// steps is how many presses of the key of mode go back to where they
// started.
func (mode playerMode) steps() int {
	if mode == shuffleMode {
		return 2
	}
	return len(repeatCycle)
}

// changeMode turns shuffle on or off, or moves repeat to its next mode,
// presses times. Presses that come back to the current mode send nothing.
func (m Model) changeMode(mode playerMode, presses int) (Model, tea.Cmd) {
	presses %= mode.steps()
	if presses == 0 {
		return m, nil
	}
	c := m.modes[mode]
	if c.busy {
		c.pending += presses
		m.modes[mode] = c
		return m, nil
	}
	// The last change is fresh while spot-go started nothing since and no
	// poll showed Spotify on another device (see followPolled). Otherwise
	// the change reads where Spotify plays, as that decides how to change
	// the mode.
	fresh := !c.at.IsZero() && time.Since(c.at) < followGrace && c.plays == *m.plays
	m.modes[mode] = modeControl{busy: true, at: c.at, device: c.device, plays: c.plays}

	changed := modeChanged{mode: mode, device: c.device, plays: *m.plays, shuffle: m.playerBar.shuffle, repeat: m.playerBar.repeat}
	ctx, client, own, ownModes := m.ctx, m.spotifyClient, m.ownDevice(), m.ownModes
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		if !fresh {
			state, err := client.Playback(ctx)
			switch {
			case err != nil:
				changed.err = err
			case state.Device == "":
				changed.err = errNoActiveDevice
			}
			if changed.err != nil {
				return changed
			}
			changed.device, changed.shuffle, changed.repeat = state.Device, state.Shuffle, state.Repeat
		}

		// Spotify answers "Restriction violated" to the Web API on spot-go's
		// own device, so spot-go changes the modes there through Soloist.
		local := ownModes != nil && own != "" && changed.device == own
		switch mode {
		case shuffleMode:
			changed.shuffle = !changed.shuffle
			if local {
				changed.err = ownModes.SetShuffle(ctx, changed.shuffle)
			} else {
				changed.err = client.SetShuffle(ctx, changed.shuffle)
			}
		case repeatMode:
			changed.repeat = nextRepeat(changed.repeat, presses)
			if local {
				changed.err = ownModes.SetRepeat(ctx, string(changed.repeat))
			} else {
				changed.err = client.SetRepeat(ctx, changed.repeat)
			}
		}
		return changed
	}
}

// nextRepeat is the repeat mode presses presses of the repeat key after
// repeat. An unknown mode counts as off.
func nextRepeat(repeat spotify.Repeat, presses int) spotify.Repeat {
	i := 0
	for j, r := range repeatCycle {
		if r == repeat {
			i = j
		}
	}
	return repeatCycle[(i+presses)%len(repeatCycle)]
}

func (m Model) modeChanged(msg modeChanged) (Model, tea.Cmd) {
	pending := m.modes[msg.mode].pending
	if msg.err != nil {
		m.modes[msg.mode] = modeControl{}
		return m.notify(msg.mode.failure()+errorText(msg.err), true)
	}

	m.modes[msg.mode] = modeControl{at: time.Now(), device: msg.device, plays: msg.plays}
	switch msg.mode {
	case shuffleMode:
		m.playerBar.shuffle = msg.shuffle
	case repeatMode:
		m.playerBar.repeat = msg.repeat
	}
	m, notice := m.notify(msg.text(), false)
	m, next := m.changeMode(msg.mode, pending)
	return m, tea.Batch(notice, next)
}

func (mode playerMode) failure() string {
	if mode == shuffleMode {
		return "Fail to change shuffle: "
	}
	return "Fail to change repeat: "
}

func (msg modeChanged) text() string {
	switch {
	case msg.mode == shuffleMode && msg.shuffle:
		return "Shuffle on"
	case msg.mode == shuffleMode:
		return "Shuffle off"
	case msg.repeat == spotify.RepeatContext:
		return "Repeat on"
	case msg.repeat == spotify.RepeatTrack:
		return "Repeat one"
	}
	return "Repeat off"
}
