package player

import (
	"time"

	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type nowPlaying struct {
	info      media.Info
	item      libspotify.URI
	at        time.Time
	commandAt time.Time
}

func (n nowPlaying) elapsed(now time.Time) time.Duration {
	elapsed := n.info.Elapsed
	if n.info.Playing {
		elapsed += now.Sub(n.at)
	}
	if n.info.Duration > 0 {
		elapsed = min(elapsed, n.info.Duration)
	}
	return elapsed
}

func (m Model) publishNowPlaying(item libspotify.URI, info media.Info, now time.Time) Model {
	if m.publish == nil {
		return m
	}
	m.nowPlaying.info, m.nowPlaying.item, m.nowPlaying.at = info, item, now
	m.publish(info)
	return m
}

func (m Model) publishCommand(item libspotify.URI, info media.Info) Model {
	now := time.Now()
	m.nowPlaying.commandAt = now
	return m.publishNowPlaying(item, info, now)
}

func (m Model) publishToggled(msg playbackToggled) Model {
	return m.publishCommand(msg.item, media.Info{Title: msg.name, Artist: msg.artist, Duration: msg.duration, Elapsed: msg.progress, Playing: !msg.paused})
}

func (m Model) publishPolled(playing spotify.NowPlaying) Model {
	now := time.Now()
	if now.Sub(m.nowPlaying.commandAt) < followGrace {
		return m
	}
	if playing.Item != "" {
		return m.publishNowPlaying(playing.Item, media.Info{
			Title:    playing.Name,
			Artist:   playing.Artist,
			Duration: playing.Duration,
			Elapsed:  playing.Progress,
			Playing:  true,
		}, now)
	}
	if !m.nowPlaying.info.Playing {
		return m
	}
	info := m.nowPlaying.info
	info.Elapsed, info.Playing = m.nowPlaying.elapsed(now), false
	if playing.PausedItem != "" && playing.PausedItem == m.nowPlaying.item {
		info.Elapsed = playing.PausedProgress
	}
	return m.publishNowPlaying(m.nowPlaying.item, info, now)
}
