package player

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	playbackToggle struct {
		busy    bool
		known   playbackToggled
		at      time.Time
		settled int
		skips   int
		// seek is how far to seek once the change that runs ends, from
		// presses while it ran.
		seek time.Duration
	}

	playbackToggled struct {
		paused   bool
		skip     bool
		seek     bool
		device   libspotify.ID
		name     string
		artist   string
		item     libspotify.URI
		progress time.Duration
		duration time.Duration
		plays    int
		settled  int
		err      error
	}

	playbackChange int
)

const (
	toggleChange playbackChange = iota
	playChange
	pauseChange
	nextChange
	previousChange
)

const (
	confirmPollEvery = 250 * time.Millisecond
	confirmPolls     = 8
	seekStep         = 10 * time.Second
)

var errNothingPlaying = errors.New("nothing is playing")

var mediaChanges = map[media.Command]playbackChange{
	media.TogglePlayPause: toggleChange,
	media.Play:            playChange,
	media.Pause:           pauseChange,
	media.NextTrack:       nextChange,
	media.PreviousTrack:   previousChange,
}

func (m Model) changePlayback(change playbackChange) (Model, tea.Cmd) {
	if m.toggle.busy {
		switch change {
		case nextChange:
			m.toggle.skips++
		case previousChange:
			m.toggle.skips--
		}
		return m, nil
	}
	m.toggle.busy = true

	plays, settled := *m.plays, m.toggle.settled
	state, fresh := m.knownPlayback()
	fallback := m.ownDevice()
	ctx, client, confirmEvery := m.ctx, m.spotifyClient, m.confirmEvery

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		if change == nextChange || change == previousChange {
			return skipPlayback(ctx, client, change == nextChange, confirmEvery, plays, settled)
		}

		if !fresh {
			var err error
			if state, err = client.Playback(ctx); err != nil {
				return playbackToggled{plays: plays, settled: settled, err: err}
			}
		}

		pause := state.Playing
		switch change {
		case playChange:
			pause = false
		case pauseChange:
			pause = true
		}
		toggled := toggledFrom(state, plays, settled)
		toggled.paused = pause
		switch {
		case pause && state.Playing:
			toggled.err = client.Pause(ctx)
		case !pause && !state.Playing:
			var target libspotify.ID
			if toggled.device == "" {
				target, toggled.device = fallback, fallback
			}
			toggled.err = client.ResumePlayback(ctx, target)
		}
		return toggled
	}
}

// knownPlayback is what Spotify plays, as the last play/pause, skip or seek
// left it. It is fresh when that change ended a moment ago and nothing
// started playing since, so a new change can skip reading it again.
func (m Model) knownPlayback() (spotify.Playback, bool) {
	known := m.toggle.known
	since := time.Since(m.toggle.at)
	fresh := !m.toggle.at.IsZero() && since < followGrace && known.plays == *m.plays && known.settled == m.toggle.settled
	return spotify.Playback{
		Playing:  !known.paused,
		Device:   known.device,
		Item:     known.item,
		Name:     known.name,
		Artist:   known.artist,
		Progress: known.position(since),
		Duration: known.duration,
	}, fresh
}

// seek moves what Spotify plays delta forward, or back when delta is
// negative, without going past its start or its end. Presses while another
// change runs add up, and seek once it ends.
func (m Model) seek(delta time.Duration) (Model, tea.Cmd) {
	if m.toggle.busy {
		m.toggle.seek += delta
		return m, nil
	}
	m.toggle.busy = true

	plays, settled := *m.plays, m.toggle.settled
	state, fresh := m.knownPlayback()
	ctx, client := m.ctx, m.spotifyClient

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		failed := playbackToggled{seek: true, plays: plays, settled: settled}
		if !fresh || state.Item == "" {
			var err error
			if state, err = client.Playback(ctx); err != nil {
				failed.err = err
				return failed
			}
		}
		if state.Item == "" {
			failed.err = errNothingPlaying
			return failed
		}

		target := max(0, state.Progress+delta)
		if state.Duration > 0 {
			target = min(target, state.Duration)
		}
		toggled := toggledFrom(state, plays, settled)
		toggled.paused, toggled.seek, toggled.progress = !state.Playing, true, target
		toggled.err = client.SeekTo(ctx, target)
		return toggled
	}
}

func skipPlayback(ctx context.Context, client *spotify.Client, next bool, every time.Duration, plays, settled int) playbackToggled {
	failed := playbackToggled{skip: true, plays: plays, settled: settled}
	before, err := client.Playback(ctx)
	if err != nil {
		failed.err = err
		return failed
	}

	skip := client.Previous
	if next {
		skip = client.Next
	}
	if failed.err = skip(ctx); failed.err != nil {
		return failed
	}

	after := before
	for range confirmPolls {
		if wait(ctx, every) != nil {
			break
		}
		state, err := client.Playback(ctx)
		if err != nil {
			break
		}
		if state.Item == "" {
			continue
		}
		after = state
		if state.Item != before.Item || state.Progress < before.Progress {
			break
		}
	}
	toggled := toggledFrom(after, plays, settled)
	toggled.paused, toggled.skip = !after.Playing, true
	return toggled
}

func (t playbackToggled) position(since time.Duration) time.Duration {
	position := t.progress
	if !t.paused {
		position += since
	}
	if t.duration > 0 {
		position = min(position, t.duration)
	}
	return position
}

// nextSkip runs the skips and seeks pressed while a change ran: the skips
// first, one at a time, and then the seeks, added up.
func (m Model) nextSkip() (Model, tea.Cmd) {
	switch {
	case m.toggle.skips > 0:
		m, cmd := m.changePlayback(nextChange)
		m.toggle.skips--
		return m, cmd
	case m.toggle.skips < 0:
		m, cmd := m.changePlayback(previousChange)
		m.toggle.skips++
		return m, cmd
	case m.toggle.seek != 0:
		delta := m.toggle.seek
		m.toggle.seek = 0
		return m.seek(delta)
	}
	return m, nil
}

func toggledFrom(state spotify.Playback, plays, settled int) playbackToggled {
	return playbackToggled{
		device:   state.Device,
		name:     state.Name,
		artist:   state.Artist,
		item:     state.Item,
		progress: state.Progress,
		duration: state.Duration,
		plays:    plays,
		settled:  settled,
	}
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (m Model) playbackToggled(msg playbackToggled) Model {
	m.toggle = playbackToggle{settled: m.toggle.settled, skips: m.toggle.skips, seek: m.toggle.seek}
	if msg.err != nil {
		m.toggle.skips, m.toggle.seek = 0, 0
		return m
	}
	m.toggle.known, m.toggle.at = msg, time.Now()
	return m
}

func (t playbackToggled) info() media.Info {
	return media.Info{Title: t.name, Artist: t.artist, Duration: t.duration, Elapsed: t.progress, Playing: !t.paused}
}
