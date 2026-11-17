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
	nowPlaying struct {
		track
		commandAt time.Time
	}

	playbackConfirmed struct {
		playing   spotify.NowPlaying
		plays     int
		commandAt time.Time
	}
)

const startSlack = time.Second

func (n nowPlaying) stillPlaying(playing spotify.NowPlaying, now time.Time) bool {
	return n.item != "" && playing.Item == n.item && playing.Progress+startSlack >= n.elapsed(now)
}

func (m Model) publishNowPlaying(item libspotify.URI, info media.Info, now time.Time) Model {
	info.ID = string(item)
	m.nowPlaying.track = track{item: item, info: info, at: now}
	if m.publish != nil {
		m.publish(info)
	}
	return m
}

func (m Model) publishCommand(item libspotify.URI, info media.Info) Model {
	now := time.Now()
	m.nowPlaying.commandAt = now
	return m.publishNowPlaying(item, info, now)
}

func (m Model) publishToggled(msg playbackToggled) Model {
	toggled := m.nowPlaying.track.toggled(msg, time.Now())
	return m.publishCommand(toggled.item, toggled.info)
}

func (m Model) publishPolled(playing spotify.NowPlaying) Model {
	now := time.Now()
	if now.Sub(m.nowPlaying.commandAt) < followGrace {
		return m
	}
	if playing.Item != "" {
		return m.publishNowPlaying(playing.Item, playingInfo(playing), now)
	}
	if playing.Playing {
		return m
	}
	paused, ok := m.nowPlaying.track.paused(playing, now)
	if !ok {
		return m
	}
	return m.publishNowPlaying(paused.item, paused.info, now)
}

func playingInfo(playing spotify.NowPlaying) media.Info {
	return media.Info{
		Title:    playing.Name,
		Artist:   playing.Artist,
		Duration: playing.Duration,
		Elapsed:  playing.Progress,
		Playing:  true,
	}
}

func (m Model) playbackStarted(msg tea.Msg, uri, item libspotify.URI, info media.Info) (Model, tea.Cmd) {
	m.toggle.settled++
	before := m.nowPlaying
	m = m.publishCommand(item, info)
	m.playerBar = m.playerBar.started(m.ownDevice())
	return m, tea.Batch(m.broadcast(msg), m.confirmStart(before, uri, item, info.Elapsed))
}

func (m Model) confirmStart(before nowPlaying, uri, item libspotify.URI, from time.Duration) tea.Cmd {
	ctx, client, every, polls := m.ctx, m.spotifyClient, m.confirmEvery, m.startPolls
	confirmed := playbackConfirmed{plays: *m.plays, commandAt: m.nowPlaying.commandAt}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		start := time.Now()
		for range polls {
			if wait(ctx, every) != nil {
				return nil
			}
			playing, err := client.NowPlaying(ctx)
			if err != nil {
				return nil
			}
			if startedPlaying(playing, uri, item, from+time.Since(start)) && !before.stillPlaying(playing, time.Now()) {
				confirmed.playing = playing
				return confirmed
			}
		}
		return nil
	}
}

func startedPlaying(playing spotify.NowPlaying, uri, item libspotify.URI, latest time.Duration) bool {
	return playing.Item != "" &&
		playing.Context == uri &&
		(item == "" || playing.Item == item) &&
		playing.Progress <= latest+followGrace
}

func (m Model) playbackConfirmed(msg playbackConfirmed) (Model, tea.Cmd) {
	if msg.plays != *m.plays || !msg.commandAt.Equal(m.nowPlaying.commandAt) {
		return m, nil
	}
	m = m.publishNowPlaying(msg.playing.Item, playingInfo(msg.playing), time.Now())
	m = m.followPolled(msg.playing)
	return m, m.broadcast(msg)
}

// skippedTo shows a skip to an item of the queue. The Queue tab already
// waited for Spotify to play the item, so there is nothing left to confirm.
// A newer play started while it skipped keeps what it shows.
func (m Model) skippedTo(msg queuePlaybackStarted) (Model, tea.Cmd) {
	m.toggle.settled++
	if msg.plays == *m.plays {
		m = m.publishCommand(msg.item, msg.info())
		m.playerBar.idle = false
	}
	return m, m.broadcast(msg)
}
