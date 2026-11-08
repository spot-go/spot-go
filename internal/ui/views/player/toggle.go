package player

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	playbackToggle struct {
		busy    bool
		known   playbackToggled
		at      time.Time
		settled int
	}

	playbackToggled struct {
		paused  bool
		device  libspotify.ID
		name    string
		item    libspotify.URI
		plays   int
		settled int
		err     error
	}
)

func (m Model) togglePlayback() (Model, tea.Cmd) {
	if m.toggle.busy {
		return m, nil
	}
	m.toggle.busy = true

	plays, settled := *m.plays, m.toggle.settled
	known := m.toggle.known
	fresh := !m.toggle.at.IsZero() && time.Since(m.toggle.at) < followGrace && known.plays == plays && known.settled == settled
	var fallback libspotify.ID
	if m.playerDevice != nil {
		fallback = m.playerDevice.ID
	}
	ctx, client := m.ctx, m.spotifyClient

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		state := spotify.Playback{Playing: !known.paused, Device: known.device, Item: known.item, Name: known.name}
		if !fresh {
			var err error
			if state, err = client.Playback(ctx); err != nil {
				return playbackToggled{plays: plays, settled: settled, err: err}
			}
		}

		toggled := playbackToggled{paused: state.Playing, device: state.Device, name: state.Name, item: state.Item, plays: plays, settled: settled}
		if state.Playing {
			toggled.err = client.Pause(ctx)
			return toggled
		}
		var target libspotify.ID
		if toggled.device == "" {
			target, toggled.device = fallback, fallback
		}
		toggled.err = client.ResumePlayback(ctx, target)
		return toggled
	}
}

func (m Model) playbackToggled(msg playbackToggled) Model {
	m.toggle = playbackToggle{settled: m.toggle.settled}
	if msg.err == nil {
		m.toggle.known, m.toggle.at = msg, time.Now()
	}
	return m
}

func (s playbackStatus) toggled(msg playbackToggled, now time.Time) playbackStatus {
	switch {
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
