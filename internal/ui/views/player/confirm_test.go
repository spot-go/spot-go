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
	h.wantView("⏸ Back in Black")
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
	h.wantView("⏸ Back in Black")
	h.wantPlayerReads(3)
}

func TestPlayStopsWaitingWhenSpotifyDoesNotShowIt(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{Title: "Rock", Playing: true})
	h.wantView("⏸ Rock")
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
	h.wantView("⏸ Rock")
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
	h.wantView("⏸ Rock")
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
	h.wantView("⏸ Paranoid")
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
	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "29:00 / 1:00:00")
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
	h.wantView("⏸ Come Together")
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
	h.wantView("⏸ Creep")
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
	h.wantView("⏸ Back in Black")

	h.settle()
	h.send(pollPlayback{})

	if got := lastInfo(t, published); got.Title != "So What" {
		t.Fatalf("now playing = %+v, want So What", got)
	}
	h.wantView("⏸ So What")
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
	h.wantView("▶ Back in Black")
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
	h.wantView("⏸ Jazz")
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
	h.wantView("⏸ Back in Black")
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
	h.wantView("⏸ Rock")
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

	h.wantView("⏸ Back in Black")
	h.wantPlayerReads(3)
}

// backInBlackResumes is a resume while nothing plays: Spotify resumes what
// played last, Back in Black, on spot-go.
func backInBlackResumes(f *fakeAPI) {
	backInBlackPlays(f)
	f.device, f.deviceName, f.volume = testDeviceID, "spot-go@laptop", 70
}

func TestResumeWithNothingPlayingShowsWhatSpotifyResumes(t *testing.T) {
	api := newFakeAPI()
	api.onResume = backInBlackResumes
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("space"))

	h.wantResume(0, testDeviceID)
	wantInfo(t, published, backInBlack)
	h.wantView("⏸ Back in Black")
	h.wantPlayerRow(1, "70%")
	h.wantPlayerReads(2)
}

func TestResumeWaitsForSpotifyToShowWhatItResumes(t *testing.T) {
	api := newFakeAPI()
	api.onResume, api.playLag = backInBlackResumes, 2
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(media.Play)

	wantInfo(t, published, backInBlack)
	h.wantView("⏸ Back in Black")
	h.wantPlayerReads(4)
}

func TestResumeStopsWaitingWhenSpotifyShowsNothing(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("space"))

	wantInfo(t, published, media.Info{Playing: true})
	h.wantView("⏸")
	h.wantPlayerReads(1 + confirmPolls)
}

func TestResumeStopsWaitingWhenSpotifyFails(t *testing.T) {
	api := newFakeAPI()
	api.onResume = func(f *fakeAPI) { f.playerStatus = http.StatusInternalServerError }
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()

	h.send(key("space"))

	h.wantView("⏸")
	h.dontWantView("Fail")
	h.wantPlayerReads(2)
}

func TestResumeOfAKnownSongNeedsNoConfirmation(t *testing.T) {
	api := playingOnPhone()
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()

	h.send(key("k"))

	h.wantResume(0, "")
	h.wantView("⏸ So What")
	h.wantPlayerReads(1)
}

func TestPlayKeyWhileSpotifyPlaysNeedsNoConfirmation(t *testing.T) {
	api := newFakeAPI()
	// Spotify plays something it doesn't name, such as an ad.
	api.playsElsewhere("spotify:playlist:p1", "", "", "")
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()

	h.send(media.Play)

	h.wantPlays(0)
	h.wantPlayerReads(1)
}

func TestPauseNeedsNoConfirmation(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)
	h.confirmStarts()

	h.send(key("space"))

	h.wantPauses("")
	h.wantPlayerReads(1)
}

func TestPlayStartedDuringAResumeKeepsItsStatus(t *testing.T) {
	api := newFakeAPI()
	api.onResume = backInBlackResumes
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	model, resume := h.model.Update(key("space"))
	h.model = model
	h.send(keys("down", "enter")...)
	h.run(resume)

	wantInfo(t, published, media.Info{Title: "Jazz", Playing: true})
	h.wantPlayerRow(0, "⏸ Jazz")
	h.dontWantPlayerRow(0, "Back in Black")
}

func TestPauseRightAfterAResumeDropsItsConfirmation(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(key("space"))

	api.set(backInBlackResumes)
	m := h.player()
	m.startPolls = confirmPolls
	confirmed := h.exec(m.confirmResume(m.toggle.known))
	if len(confirmed) != 1 {
		t.Fatalf("confirmation = %v, want one message", confirmed)
	}
	h.send(key("space"))
	h.send(confirmed...)

	h.wantPauses("")
	if got := lastInfo(t, published); got.Playing || got.Title != "" {
		t.Fatalf("now playing = %+v, want nothing playing", got)
	}
	h.wantPlayerRow(0, "▶")
	h.dontWantPlayerRow(0, "Back in Black")
}

func TestPlayRightAfterAResumeDropsItsConfirmation(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(key("space"))

	api.set(backInBlackResumes)
	m := h.player()
	m.startPolls = confirmPolls
	confirmed := h.exec(m.confirmResume(m.toggle.known))
	if len(confirmed) != 1 {
		t.Fatalf("confirmation = %v, want one message", confirmed)
	}
	h.send(keys("down", "enter")...)
	h.send(confirmed...)

	wantInfo(t, published, media.Info{Title: "Jazz", Playing: true})
	h.wantView("⏸ Jazz")
}
