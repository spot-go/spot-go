package player

import (
	"context"
	"errors"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	volumeControl struct {
		busy    bool
		pending []int
		known   volumeChanged
		at      time.Time
	}

	volumeChanged struct {
		device  libspotify.ID
		percent int
		plays   int
		settled int
		err     error
	}

	volumeSaver struct {
		mu      sync.Mutex
		percent int
		save    func(int) error
	}
)

const (
	volumeStep    = 5
	maxVolume     = 100
	unsavedVolume = -1
)

var (
	errNoActiveDevice  = errors.New("no active device")
	errNoVolumeControl = errors.New("the device has no volume control")
)

func (m Model) changeVolume(deltas ...int) (Model, tea.Cmd) {
	if m.volume.busy {
		m.volume.pending = append(m.volume.pending, deltas...)
		return m, nil
	}
	m.volume.busy = true

	plays, settled := *m.plays, m.toggle.settled
	known := m.volume.known
	fresh := !m.volume.at.IsZero() && time.Since(m.volume.at) < followGrace && known.plays == plays && known.settled == settled
	ctx, client := m.ctx, m.spotifyClient

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		changed := volumeChanged{device: known.device, percent: known.percent, plays: plays, settled: settled}
		if !fresh {
			state, err := client.Playback(ctx)
			switch {
			case err != nil:
				changed.err = err
			case !state.SupportsVolume && state.Device == "":
				changed.err = errNoActiveDevice
			case !state.SupportsVolume:
				changed.err = errNoVolumeControl
			}
			if changed.err != nil {
				return changed
			}
			changed.device, changed.percent = state.Device, state.Volume
		}

		target := stepVolume(changed.percent, deltas)
		if target != changed.percent {
			changed.err = client.SetVolume(ctx, changed.device, target)
		}
		changed.percent = target
		return changed
	}
}

func stepVolume(percent int, deltas []int) int {
	for _, delta := range deltas {
		percent = max(0, min(maxVolume, percent+delta))
	}
	return percent
}

func (m Model) volumeChanged(msg volumeChanged) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if msg.err != nil {
		m.volume = volumeControl{}
		var notice tea.Cmd
		m, notice = m.notify("Fail to change the volume: "+errorText(msg.err), true)
		cmds = append(cmds, notice)
	} else {
		m.volume.busy = false
		m.volume.known, m.volume.at = msg, time.Now()
		m.playerBar.volume, m.playerBar.hasVolume = msg.percent, true
		cmds = append(cmds, m.rememberVolume(msg.device, msg.percent))
	}

	if pending := m.volume.pending; len(pending) > 0 {
		m.volume.pending = nil
		var next tea.Cmd
		m, next = m.changeVolume(pending...)
		cmds = append(cmds, next)
	}
	return m, tea.Batch(cmds...)
}

// changing reports whether spot-go is changing the volume, or changed it a
// moment ago, so what Spotify reports may not show the change yet.
func (v volumeControl) changing() bool {
	return v.busy || !v.at.IsZero() && time.Since(v.at) < followGrace
}

func (m Model) rememberPolledVolume(msg playbackPolled) tea.Cmd {
	if msg.err != nil || !msg.playing.SupportsVolume || m.volume.changing() {
		return nil
	}
	return m.rememberVolume(msg.playing.Device, msg.playing.Volume)
}

func (m Model) rememberVolume(device libspotify.ID, percent int) tea.Cmd {
	if m.playerDevice == nil || device != m.playerDevice.ID {
		return nil
	}
	return m.volumeSaver.remember(percent)
}

func newVolumeSaver(save func(int) error) *volumeSaver {
	if save == nil {
		return nil
	}
	return &volumeSaver{percent: unsavedVolume, save: save}
}

func (s *volumeSaver) remember(percent int) tea.Cmd {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if percent == s.percent {
		return nil
	}
	s.percent = percent
	return s.write
}

func (s *volumeSaver) write() tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.percent != unsavedVolume && s.save(s.percent) != nil {
		s.percent = unsavedVolume
	}
	return nil
}
