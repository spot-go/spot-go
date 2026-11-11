package player

import (
	"net/http"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/media"
	libspotify "github.com/zmb3/spotify/v2"
)

func (h *harness) confirmStarts() {
	m := h.player()
	m.startPolls = confirmPolls
	h.model = m
}

func (h *harness) confirmation(uri, item string, from time.Duration) []tea.Msg {
	h.t.Helper()

	m := h.player()
	m.startPolls = confirmPolls
	return h.exec(m.confirmStart(nowPlaying{}, libspotify.URI(uri), libspotify.URI(item), from))
}

func backInBlackPlays(f *fakeAPI) {
	f.playing, f.playingType, f.playingItem, f.playingName = "spotify:playlist:p0", "", albumTrackURI("Back in Black"), "Back in Black"
	f.playingArtist, f.progressMs, f.durationMs, f.paused = "AC/DC", 0, 255000, false
}

var backInBlack = media.Info{ID: albumTrackURI("Back in Black"), Title: "Back in Black", Artist: "AC/DC", Duration: 255 * time.Second, Playing: true}

func wantInfo(t *testing.T, published *[]media.Info, want media.Info) {
	t.Helper()

	if got := lastInfo(t, published); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}
}

func TestPlayShowsTheSongSpotifyStarts(t *testing.T) {
	api := playingOnPhone()
	api.onPlay = backInBlackPlays
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, backInBlack)
	h.wantView("Playing: Back in Black")
	h.wantPlayerReads(1)
}

func TestPlayWaitsForSpotifyToShowIt(t *testing.T) {
	api := playingOnPhone()
	api.onPlay, api.playLag = backInBlackPlays, 2
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, backInBlack)
	h.wantView("Playing: Back in Black")
	h.wantPlayerReads(3)
}

func TestPlayStopsWaitingWhenSpotifyDoesNotShowIt(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{Title: "Rock", Playing: true})
	h.wantView("Playing: Rock")
	h.wantPlayerReads(confirmPolls)
}

func TestPlayStopsWaitingWhenSpotifyFails(t *testing.T) {
	api := playingOnPhone()
	api.playerStatus = http.StatusInternalServerError
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{Title: "Rock", Playing: true})
	h.wantView("Playing: Rock")
	h.wantPlayerReads(1)
}

func TestPlayIgnoresTheSongPlayingBeforeIt(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p0", "", albumTrackURI("Highway Star"), "Highway Star")
	api.progressMs, api.durationMs = 120000, 365000
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{Title: "Rock", Playing: true})
	h.wantView("Playing: Rock")
	h.wantPlayerReads(confirmPolls)
}

func TestSongPlayWaitsForThatSong(t *testing.T) {
	api := newFakeAPI()
	backInBlackPlays(api)
	api.onPlay = func(f *fakeAPI) {
		f.playingItem, f.playingName, f.playingArtist, f.durationMs = albumTrackURI("Paranoid"), "Paranoid", "Black Sabbath", 168000
	}
	api.playLag = 1
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(keys("tab", "down", "enter")...)

	wantInfo(t, published, media.Info{ID: albumTrackURI("Paranoid"), Title: "Paranoid", Artist: "Black Sabbath", Duration: 168 * time.Second, Playing: true})
	h.wantView("Playing: Paranoid")
	h.wantPlayerReads(2)
}

func TestEpisodePlayShowsItsLength(t *testing.T) {
	api := newFakeAPI()
	api.onPlay = func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName = "spotify:show:s0", "episode", episodeURI("Inside the Final Hours"), "Inside the Final Hours"
		f.progressMs, f.durationMs = 1740000, 3600000
	}
	h := newPodcastHarness(t, api)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(keys("tab", "down", "enter")...)

	wantInfo(t, published, media.Info{ID: episodeURI("Inside the Final Hours"), Title: "Inside the Final Hours", Duration: time.Hour, Elapsed: 29 * time.Minute, Playing: true})
	h.wantView("Playing: Inside the Final Hours from 29:00")
}

func TestAlbumPlayShowsTheSongSpotifyStarts(t *testing.T) {
	api := newFakeAPI()
	api.onPlay = func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName = "spotify:album:a0", "album", albumTrackURI("Come Together"), "Come Together"
		f.playingArtist, f.durationMs = "The Beatles", 259000
	}
	h := newAlbumsHarness(t, api)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{ID: albumTrackURI("Come Together"), Title: "Come Together", Artist: "The Beatles", Duration: 259 * time.Second, Playing: true})
	h.wantView("Playing: Come Together")
}

func TestArtistPlayShowsTheSongSpotifyStarts(t *testing.T) {
	api := newFakeAPI()
	api.onPlay = func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName = "spotify:artist:ar0", "artist", albumTrackURI("Creep"), "Creep"
		f.playingArtist, f.durationMs = "Radiohead", 238000
	}
	h := newArtistHarness(t, api)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{ID: albumTrackURI("Creep"), Title: "Creep", Artist: "Radiohead", Duration: 238 * time.Second, Playing: true})
	h.wantView("Playing: Creep")
}

func TestPollRightAfterAConfirmedPlayKeepsIt(t *testing.T) {
	api := playingOnPhone()
	api.onPlay = backInBlackPlays
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()
	h.send(key("enter"))

	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	h.send(pollPlayback{})

	wantInfo(t, published, backInBlack)
	h.wantView("Playing: Back in Black")

	h.settle()
	h.send(pollPlayback{})

	if got := lastInfo(t, published); got.Title != "So What" {
		t.Fatalf("now playing = %+v, want So What", got)
	}
	h.wantView("Playing: So What")
}

func TestToggleRightAfterAPlayDropsItsConfirmation(t *testing.T) {
	api := playingOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(key("enter"))

	api.set(backInBlackPlays)
	confirmed := h.confirmation("spotify:playlist:p0", "", 0)
	if len(confirmed) != 1 {
		t.Fatalf("confirmation = %v, want one message", confirmed)
	}
	h.send(key("space"))
	h.send(confirmed...)

	if got := lastInfo(t, published); got.Playing || got.Title != "Back in Black" {
		t.Fatalf("now playing = %+v, want Back in Black paused", got)
	}
	h.wantView("Paused: Back in Black")
}

func TestNewerPlayDropsTheConfirmation(t *testing.T) {
	api := playingOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(key("enter"))

	api.set(backInBlackPlays)
	confirmed := h.confirmation("spotify:playlist:p0", "", 0)
	if len(confirmed) != 1 {
		t.Fatalf("confirmation = %v, want one message", confirmed)
	}
	h.send(keys("down", "enter")...)
	h.send(confirmed...)

	wantInfo(t, published, media.Info{Title: "Jazz", Playing: true})
	h.wantView("Playing: Jazz")
}

func TestPlayIgnoresTheSongThatJustStartedBeforeIt(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p0", "", albumTrackURI("Highway Star"), "Highway Star")
	api.progressMs, api.durationMs = 1000, 365000
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	api.set(func(f *fakeAPI) { f.onPlay, f.playLag = backInBlackPlays, 1 })
	h.send(key("enter"))

	wantInfo(t, published, backInBlack)
	h.wantView("Playing: Back in Black")
	h.wantPlayerReads(3)
}

func TestPlayAcceptsSpotifyBeingAhead(t *testing.T) {
	api := playingOnPhone()
	api.onPlay = func(f *fakeAPI) {
		backInBlackPlays(f)
		f.progressMs = 2000
	}
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	want := backInBlack
	want.Elapsed = 2 * time.Second
	wantInfo(t, published, want)
}

func TestPlayWaitsForTheSongWhileTheContextLoads(t *testing.T) {
	api := playingOnPhone()
	api.onPlay = func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName, f.progressMs = "spotify:playlist:p0", "", "", "", 0
	}
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{Title: "Rock", Playing: true})
	h.wantView("Playing: Rock")
	h.wantPlayerReads(confirmPolls)
}

func TestPlayIgnoresTheSongThatJustStartedWithoutNowPlaying(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p0", "", albumTrackURI("Highway Star"), "Highway Star")
	api.progressMs, api.durationMs = 1000, 365000
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	h.send(pollPlayback{})

	api.set(func(f *fakeAPI) { f.onPlay, f.playLag = backInBlackPlays, 1 })
	h.send(key("enter"))

	h.wantView("Playing: Back in Black")
	h.wantPlayerReads(3)
}
