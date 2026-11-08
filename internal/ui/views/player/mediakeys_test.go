package player

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/media"
)

func (f *fakeAPI) skipRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.skips...)
}

func (h *harness) wantSkips(skips ...string) {
	h.t.Helper()

	if got := h.api.skipRequests(); !slices.Equal(got, skips) {
		h.t.Fatalf("skips = %q, want %q", got, skips)
	}
}

func (h *harness) recordNowPlaying() *[]media.Info {
	published := new([]media.Info)
	m := h.player()
	m.publish = func(info media.Info) { *published = append(*published, info) }
	h.model = m
	return published
}

func lastInfo(t *testing.T, published *[]media.Info) media.Info {
	t.Helper()

	if len(*published) == 0 {
		t.Fatal("nothing was published to Now Playing")
	}
	return (*published)[len(*published)-1]
}

func soWhatOnPhone() *fakeAPI {
	api := playingOnPhone()
	api.playingArtist = "Miles Davis"
	api.progressMs, api.durationMs = 60000, 562000
	return api
}

func TestMediaPlayPauseKeyPauses(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(media.TogglePlayPause)

	h.wantPauses("")
	h.wantView("Paused: So What")
}

func TestMediaPauseWhilePausedSendsNothing(t *testing.T) {
	api := playingOnPhone()
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.Pause)

	h.wantPauses()
	h.wantPlays(0)
	h.wantView("Paused: So What")
}

func TestMediaPlayResumesOnlyWhenPaused(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(media.Play)
	h.wantPlays(0)
	h.wantPauses()
	h.wantView("Playing: So What")

	h.api.set(func(f *fakeAPI) { f.paused = true })
	h.forgetToggle()
	h.send(media.Play)
	h.wantResume(0, "")
}

func TestMediaKeysWorkWhileFiltering(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(key("/"), media.TogglePlayPause)

	h.wantPauses("")
	if got := h.playlists().list.FilterValue(); got != "" {
		t.Fatalf("filter = %q, want it empty", got)
	}
}

func TestUnknownMediaCommandIsIgnored(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(media.Command(99))

	h.wantPlayerReads(0)
	h.wantPauses()
	h.wantSkips()
}

func TestNextTrackSkips(t *testing.T) {
	api := playingOnPhone()
	api.nextItem, api.nextName = "spotify:track:freddie", "Freddie Freeloader"
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.NextTrack)

	h.wantSkips("next")
	h.wantPlayerReads(2)
	h.wantView("Playing: Freddie Freeloader")
}

func TestPreviousTrackStopsWaitingWhenTheSongRestarts(t *testing.T) {
	api := playingOnPhone()
	api.progressMs = 60000
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.PreviousTrack)

	h.wantSkips("previous")
	h.wantPlayerReads(2)
	h.wantView("Playing: So What")
}

func TestSkipWaitsAWhileForTheNewSong(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(media.NextTrack)

	h.wantSkips("next")
	h.wantPlayerReads(1 + skipPolls)
	h.wantView("Playing: So What")
}

func TestSkipErrorsAreShown(t *testing.T) {
	api := playingOnPhone()
	api.skipStatus = http.StatusNotFound
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.NextTrack)

	h.wantView("Fail to skip: Player command failed: No active device found")
	h.send(media.TogglePlayPause)
	h.wantPlayerReads(2)
	h.wantPauses("")
}

func TestPlayPauseRightAfterASkipUsesTheNewSong(t *testing.T) {
	api := playingOnPhone()
	api.nextItem, api.nextName = "spotify:track:freddie", "Freddie Freeloader"
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.NextTrack)
	h.send(media.TogglePlayPause)

	h.wantPlayerReads(2)
	h.wantPauses("")
	h.wantView("Paused: Freddie Freeloader")
}

func TestNowPlayingFollowsPolls(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	published := h.recordNowPlaying()

	h.send(pollPlayback{})

	want := media.Info{Title: "So What", Artist: "Miles Davis", Duration: 562 * time.Second, Elapsed: time.Minute, Playing: true}
	if got := lastInfo(t, published); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}
}

func TestNowPlayingShowsAPause(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.send(key("space"))

	got := lastInfo(t, published)
	if got.Playing || got.Title != "So What" || got.Artist != "Miles Davis" || got.Duration != 562*time.Second {
		t.Fatalf("now playing = %+v, want So What paused", got)
	}
	if got.Elapsed < time.Minute || got.Elapsed > time.Minute+time.Second {
		t.Fatalf("elapsed = %v, want about 1m", got.Elapsed)
	}
}

func TestNowPlayingShowsWhatSpotGoStarts(t *testing.T) {
	h := newHarness(t, 120, 40)
	published := h.recordNowPlaying()

	h.send(key("enter"))
	if got, want := lastInfo(t, published), (media.Info{Title: "Rock", Playing: true}); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}

	h.send(keys("tab", "down", "enter")...)
	if got := lastInfo(t, published); got.Title != "Paranoid" || !got.Playing {
		t.Fatalf("now playing = %+v, want Paranoid playing", got)
	}
}

func TestNowPlayingShowsTheEpisodePosition(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	published := h.recordNowPlaying()

	h.send(keys("tab", "down", "enter")...)

	if got, want := lastInfo(t, published), (media.Info{Title: "Inside the Final Hours", Elapsed: 29 * time.Minute, Playing: true}); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}
}

func TestNowPlayingIgnoresPollsRightAfterACommand(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()

	h.send(key("enter"), pollPlayback{})
	if got := lastInfo(t, published); got.Title != "Rock" {
		t.Fatalf("now playing = %+v, want Rock", got)
	}

	h.settle()
	h.send(pollPlayback{})
	if got := lastInfo(t, published); got.Title != "So What" {
		t.Fatalf("now playing = %+v, want So What", got)
	}
}

func TestNowPlayingShowsWhenSpotifyStops(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	api.set(func(f *fakeAPI) { f.paused = true })
	h.send(pollPlayback{})
	h.send(pollPlayback{})

	if got := lastInfo(t, published); got.Playing || got.Title != "So What" {
		t.Fatalf("now playing = %+v, want So What paused", got)
	}
	if len(*published) != 2 {
		t.Fatalf("published %d times, want 2: a stopped player is published once", len(*published))
	}
}

func TestNowPlayingShowsASkip(t *testing.T) {
	api := soWhatOnPhone()
	api.nextItem, api.nextName = "spotify:track:freddie", "Freddie Freeloader"
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.send(media.NextTrack)

	got := lastInfo(t, published)
	if got.Title != "Freddie Freeloader" || !got.Playing || got.Elapsed != 0 {
		t.Fatalf("now playing = %+v, want Freddie Freeloader from the start", got)
	}
}

func TestNowPlayingIgnoresAPollSentBeforeAPlay(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	published := h.recordNowPlaying()

	poll := h.playlists().fetchPlaying()
	h.send(key("enter"))
	h.settle()
	h.run(poll)

	if got := lastInfo(t, published); got.Title != "Rock" {
		t.Fatalf("now playing = %+v, want Rock", got)
	}
}

func (h *harness) ageNowPlaying(d time.Duration) {
	m := h.player()
	m.nowPlaying.at = m.nowPlaying.at.Add(-d)
	h.model = m
}

func wantElapsed(t *testing.T, got media.Info, from, to time.Duration) {
	t.Helper()

	if got.Elapsed < from || got.Elapsed > to {
		t.Fatalf("elapsed = %v, want between %v and %v (%+v)", got.Elapsed, from, to, got)
	}
}

func TestPlayPauseRightAfterAToggleKeepsThePosition(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.send(key("space"))
	h.send(key("space"))
	got := lastInfo(t, published)
	if !got.Playing {
		t.Fatalf("now playing = %+v, want playing", got)
	}
	wantElapsed(t, got, time.Minute, time.Minute+time.Second)

	m := h.player()
	m.toggle.at = m.toggle.at.Add(-2 * time.Second)
	h.model = m
	h.send(media.Pause)
	got = lastInfo(t, published)
	if got.Playing {
		t.Fatalf("now playing = %+v, want paused", got)
	}
	wantElapsed(t, got, 62*time.Second, 63*time.Second)
	h.wantPlayerReads(2)
}

func TestSkipsPressedWhileSkippingAreQueued(t *testing.T) {
	api := playingOnPhone()
	api.nextItem, api.nextName = "spotify:track:freddie", "Freddie Freeloader"
	h := newHarnessWith(t, api, 120, 40)

	model, skip := h.model.Update(media.NextTrack)
	h.model = model
	model, queued := h.model.Update(media.NextTrack)
	h.model = model
	if queued != nil {
		t.Fatal("a second skip started while the first was running")
	}
	h.run(skip)

	h.wantSkips("next", "next")
}

func TestOppositeSkipsCancelOut(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	model, skip := h.model.Update(media.NextTrack)
	h.model = model
	for _, c := range []media.Command{media.NextTrack, media.PreviousTrack} {
		model, _ = h.model.Update(c)
		h.model = model
	}
	h.run(skip)

	h.wantSkips("next")
}

func TestFailedSkipDropsTheQueuedSkips(t *testing.T) {
	api := playingOnPhone()
	api.skipStatus = http.StatusNotFound
	h := newHarnessWith(t, api, 120, 40)

	model, skip := h.model.Update(media.NextTrack)
	h.model = model
	model, _ = h.model.Update(media.NextTrack)
	h.model = model
	h.run(skip)

	h.wantSkips("next")
	h.wantView("Fail to skip")
}

func TestNowPlayingShowsWhereSpotifyPausedElsewhere(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	h.ageNowPlaying(28 * time.Second)
	api.set(func(f *fakeAPI) { f.paused, f.progressMs = true, 62000 })
	h.send(pollPlayback{})

	got := lastInfo(t, published)
	if got.Playing || got.Elapsed != 62*time.Second {
		t.Fatalf("now playing = %+v, want paused at 1:02", got)
	}
}

func TestNowPlayingGuessesThePositionWhenNothingPlays(t *testing.T) {
	for _, tt := range []struct {
		age      time.Duration
		from, to time.Duration
	}{
		{5 * time.Second, 65 * time.Second, 66 * time.Second},
		{10 * time.Minute, 562 * time.Second, 562 * time.Second},
	} {
		api := soWhatOnPhone()
		h := newHarnessWith(t, api, 120, 40)
		published := h.recordNowPlaying()
		h.send(pollPlayback{})

		h.ageNowPlaying(tt.age)
		api.playsElsewhere("", "", "", "")
		h.send(pollPlayback{})

		got := lastInfo(t, published)
		if got.Playing || got.Title != "So What" || got.Artist != "Miles Davis" || got.Duration != 562*time.Second {
			t.Fatalf("now playing = %+v, want So What paused", got)
		}
		wantElapsed(t, got, tt.from, tt.to)
	}
}

func TestNowPlayingIgnoresAToggleSentBeforeAPlay(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	published := h.recordNowPlaying()

	model, toggle := h.model.Update(key("space"))
	h.model = model
	h.send(key("enter"))
	h.run(toggle)

	if got := lastInfo(t, published); got.Title != "Rock" || !got.Playing {
		t.Fatalf("now playing = %+v, want Rock playing", got)
	}
}
