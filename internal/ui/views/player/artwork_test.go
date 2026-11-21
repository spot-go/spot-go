package player

import (
	"testing"
	"time"

	"github.com/franciscocpg/spot-go/internal/media"
)

func TestNowPlayingShowsTheArtworkSpotifyReports(t *testing.T) {
	api := soWhatOnPhone()
	api.covers = true
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()

	h.send(pollPlayback{})

	wantInfo(t, published, media.Info{ID: "spotify:track:so-what", Title: "So What", Artist: "Miles Davis", Artwork: coverURL("spotify:track:so-what"), Duration: 562 * time.Second, Elapsed: time.Minute, Playing: true})
}

func TestNowPlayingKeepsTheArtworkThroughAPause(t *testing.T) {
	api := soWhatOnPhone()
	api.covers = true
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.send(media.Pause)

	if got := lastInfo(t, published); got.Playing || got.Artwork != coverURL("spotify:track:so-what") {
		t.Fatalf("now playing = %+v, want So What paused with its artwork", got)
	}
	h.send(media.Play)
	if got := lastInfo(t, published); !got.Playing || got.Artwork != coverURL("spotify:track:so-what") {
		t.Fatalf("now playing = %+v, want So What playing with its artwork", got)
	}
}

func TestNowPlayingShowsTheArtworkOfTheNextSong(t *testing.T) {
	api := soWhatOnPhone()
	api.covers = true
	api.nextItem, api.nextName = "spotify:track:freddie", "Freddie Freeloader"
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.send(media.NextTrack)

	if got := lastInfo(t, published); got.Title != "Freddie Freeloader" || got.Artwork != coverURL("spotify:track:freddie") {
		t.Fatalf("now playing = %+v, want Freddie Freeloader with its artwork", got)
	}
}

func TestNowPlayingShowsTheArtworkOnceSpotifyConfirmsAPlay(t *testing.T) {
	api := playingOnPhone()
	api.covers = true
	api.onPlay = backInBlackPlays
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	want := backInBlack
	want.Artwork = coverURL(albumTrackURI("Back in Black"))
	wantInfo(t, published, want)
}

func TestNowPlayingShowsTheArtworkOfAnEpisode(t *testing.T) {
	api := soWhatOnPhone()
	api.covers = true
	api.playing, api.playingType, api.playingItem, api.playingName = "spotify:show:daily", "episode", "spotify:episode:vote", "How the Vote Count Works"
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()

	h.send(pollPlayback{})

	if got := lastInfo(t, published); got.Title != "How the Vote Count Works" || got.Artwork != coverURL("spotify:episode:vote") {
		t.Fatalf("now playing = %+v, want the episode with its artwork", got)
	}
}

func TestQueueSkipShowsTheArtwork(t *testing.T) {
	api := queueAPI()
	api.covers = true
	h := newQueueHarness(t, api)
	published := h.recordNowPlaying()

	h.send(keys("down", "down", "enter")...)

	wantInfo(t, published, media.Info{ID: albumTrackURI("Oh! Darling"), Title: "Oh! Darling", Artist: "The Beatles", Artwork: coverURL(albumTrackURI("Oh! Darling")), Duration: 206 * time.Second, Playing: true})
}

func TestNowPlayingKeepsTheArtworkWhenTheSameSongStartsAgain(t *testing.T) {
	m := newHarness(t, 120, 40).player()
	var published []media.Info
	m.publish = func(info media.Info) { published = append(published, info) }
	m = m.publishNowPlaying("spotify:track:so-what", media.Info{Title: "So What", Artwork: coverURL("spotify:track:so-what"), Playing: true}, time.Now())

	m = m.publishNowPlaying("spotify:track:so-what", media.Info{Title: "So What", Playing: true}, time.Now())
	if got := published[len(published)-1].Artwork; got != coverURL("spotify:track:so-what") {
		t.Fatalf("artwork = %q, want So What's", got)
	}

	m.publishNowPlaying("spotify:track:freddie", media.Info{Title: "Freddie Freeloader", Playing: true}, time.Now())
	if got := published[len(published)-1].Artwork; got != "" {
		t.Fatalf("artwork = %q, want none until Spotify reports Freddie Freeloader's", got)
	}
}
