package player

import (
	"context"
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
	}

	playbackToggled struct {
		paused   bool
		skip     bool
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
	skipPollEvery = 250 * time.Millisecond
	skipPolls     = 8
)

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
	known := m.toggle.known
	since := time.Since(m.toggle.at)
	fresh := !m.toggle.at.IsZero() && since < followGrace && known.plays == plays && known.settled == settled
	progress := known.position(since)
	var fallback libspotify.ID
	if m.playerDevice != nil {
		fallback = m.playerDevice.ID
	}
	ctx, client, skipEvery := m.ctx, m.spotifyClient, m.skipEvery

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		if change == nextChange || change == previousChange {
			return skipPlayback(ctx, client, change == nextChange, skipEvery, plays, settled)
		}

		state := spotify.Playback{Playing: !known.paused, Device: known.device, Item: known.item, Name: known.name, Artist: known.artist, Progress: progress, Duration: known.duration}
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
	for range skipPolls {
		if wait(ctx, every) != nil {
			break
		}
		state, err := client.Playback(ctx)
		if err != nil {
			break
		}
		after = state
		if state.Item != before.Item || state.Progress < before.Progress {
			break
		}
	}
	toggled := toggledFrom(after, plays, settled)
	toggled.paused = !after.Playing
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
	m.toggle = playbackToggle{settled: m.toggle.settled, skips: m.toggle.skips}
	if msg.err != nil {
		m.toggle.skips = 0
		return m
	}
	m.toggle.known, m.toggle.at = msg, time.Now()
	return m
}

func (s playbackStatus) toggled(msg playbackToggled, now time.Time) playbackStatus {
	switch {
	case msg.err != nil && msg.skip:
		return playbackStatus{seq: s.seq, text: "Fail to skip: " + errorText(msg.err), err: true}
	case msg.err != nil:
		return playbackStatus{seq: s.seq, text: "Fail to play/pause: " + errorText(msg.err), err: true}
	case msg.paused:
		return playbackStatus{seq: s.seq, text: withName("Paused", msg.name), item: msg.item, paused: true, since: now}
	}
	return playbackStatus{seq: s.seq, text: withName("Playing", msg.name), item: msg.item, since: now}
}

func withName(text, name string) string {
	if name == "" {
		return text
	}
	return text + ": " + name
}
