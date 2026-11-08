package player

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/spotify"
)

func (f *fakeAPI) playsElsewhere(context, kind, item, name string) {
	f.set(func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName = context, kind, item, name
	})
}

func (h *harness) settle() {
	m := h.player()
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.playback.since = time.Time{}
	m.tabs[playlistsTab].content = playlists
	podcasts := m.tabs[podcastsTab].content.(podcastModel)
	podcasts.playback.since = time.Time{}
	m.tabs[podcastsTab].content = podcasts
	h.model = m
}

func TestStatusFollowsTheNextEpisode(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("tab", "down", "enter")...)
	h.wantView("Playing: Inside the Final Hours from 29:00")

	api.playsElsewhere("spotify:show:s0", "show", episodeURI("How the Vote Count Works"), "How the Vote Count Works")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("Playing: How the Vote Count Works")
	h.dontWantView("from 29:00")
	h.send(key("left"))
	h.wantView("Playing: How the Vote Count Works")
}

func TestStatusKeepsTheEpisodeItStarted(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("tab", "down", "enter")...)

	api.playsElsewhere("spotify:show:s0", "show", episodeURI("Inside the Final Hours"), "Inside the Final Hours")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("Playing: Inside the Final Hours from 29:00")
}

func TestStatusFollowsTheNextSong(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("enter"))
	h.wantView("Playing: Rock")

	api.playsElsewhere("spotify:playlist:p0", "", "spotify:track:paranoid", "Paranoid")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("Playing: Paranoid")
}

func TestStatusFollowsAnEpisodeOutsideAShow(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	api.playsElsewhere("spotify:show:s1", "episode", episodeURI("Episode 7"), "Episode 7")
	h.send(pollPlayback{})

	h.wantView("Playing: Episode 7")
	h.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
}

func TestPollSentBeforeAPlayKeepsTheStatus(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	h := newHarnessWith(t, api, 120, 40)

	poll := h.playlists().fetchPlaying()
	h.send(key("enter"))
	h.settle()
	h.run(poll)
	h.wantView("Playing: Rock")
	h.dontWantView("Playing: So What")

	h.send(pollPlayback{})
	h.wantView("Playing: So What")
}

func TestPollRightAfterAPlayKeepsTheStatus(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("enter"))
	h.send(pollPlayback{})

	h.wantView("Playing: Rock")
	h.dontWantView("Playing: So What")
}

func TestStartingStatusIsNotReplaced(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.holdTimers = true
	h.send(key("down"))

	model, load := h.model.Update(key("enter"))
	h.model = model
	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	h.send(pollPlayback{})
	h.wantView("Starting Go Weekly...")

	h.run(load)
	h.wantView("Playing: Episode 1")
}

func TestCancelledPendingPlayClearsItsStatus(t *testing.T) {
	for _, status := range []int{0, http.StatusInternalServerError} {
		api := newFakeAPI()
		api.songsStatus["s1"] = status
		h := newPodcastHarness(t, api)
		h.holdTimers = true
		h.send(key("down"))

		model, load := h.model.Update(key("enter"))
		h.model = model
		h.send(keys("left", "enter", "right")...)
		h.wantView("Starting Go Weekly...")

		h.run(load)
		h.dontWantView("Starting Go Weekly...", "Fail to play")
		h.wantPlays(1)
	}
}

func TestUnplayableSongDoesNotCancelThePendingPodcastPlay(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = []string{`{"is_local":false,"item":{"type":"track","name":"Gone","uri":"spotify:track:gone","duration_ms":1000,"is_playable":false,"artists":[]}}`}
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("right"))
	h.holdTimers = true
	h.send(key("down"))

	model, load := h.model.Update(key("enter"))
	h.model = model
	h.send(keys("left", "tab", "enter")...)
	h.wantView("Gone is unavailable")

	h.send(key("right"))
	h.run(load)
	h.wantEpisodePlay(0, "spotify:show:s1", "Episode 1", 0)
}

func TestErrorsStayWhileOtherItemsPlay(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newPodcastHarness(t, api)
	h.send(key("enter"))

	api.playsElsewhere("spotify:show:s1", "show", episodeURI("Episode 1"), "Episode 1")
	h.send(pollPlayback{})

	h.wantView("Fail to play", "Premium required")
	h.dontWantView("Playing: Episode 1")
}

func TestNothingPlayingKeepsTheStatus(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(key("enter"))

	api.set(func(f *fakeAPI) { f.paused = true })
	api.playsElsewhere("spotify:show:s1", "show", episodeURI("Episode 1"), "Episode 1")
	h.send(pollPlayback{})
	api.playsElsewhere("", "", "", "")
	h.send(pollPlayback{})

	h.wantView("Playing: How the Vote Count Works")
}

func TestNextPoll(t *testing.T) {
	for _, tt := range []struct {
		playing spotify.NowPlaying
		want    time.Duration
	}{
		{spotify.NowPlaying{}, time.Minute},
		{spotify.NowPlaying{Progress: 170 * time.Second, Duration: 3 * time.Minute}, 11 * time.Second},
		{spotify.NowPlaying{Progress: 3*time.Minute - 100*time.Millisecond, Duration: 3 * time.Minute}, 1100 * time.Millisecond},
		{spotify.NowPlaying{Duration: time.Hour}, time.Minute},
		{spotify.NowPlaying{Progress: 3 * time.Minute, Duration: 3 * time.Minute}, time.Minute},
		{spotify.NowPlaying{Progress: 4 * time.Minute, Duration: 3 * time.Minute}, time.Minute},
	} {
		if got := nextPoll(time.Minute, tt.playing); got != tt.want {
			t.Errorf("nextPoll(%+v) = %v, want %v", tt.playing, got, tt.want)
		}
	}
}

func TestPollIsScheduledForTheEndOfTheEpisode(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:show:s0", "show", episodeURI("Inside the Final Hours"), "Inside the Final Hours")
	api.progressMs, api.durationMs = 3590000, 3600000
	m := newModelWith(api, 120, 40)
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.pollEvery = time.Minute
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m, holdTimers: true}

	h.run(m.Init())

	if want := []time.Duration{11 * time.Second}; !reflect.DeepEqual(h.delays, want) {
		t.Fatalf("delays = %v, want %v", h.delays, want)
	}
}
