package player

import (
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
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
	m.nowPlaying.commandAt = time.Time{}
	h.model = m
}

func TestStatusFollowsTheNextEpisode(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("tab", "down", "enter")...)
	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "29:00 / 1:00:00")

	api.playsElsewhere("spotify:show:s0", "show", episodeURI("How the Vote Count Works"), "How the Vote Count Works")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("⏸ How the Vote Count Works")
	h.dontWantView("29:00 / 1:00:00")
	h.send(key("left"))
	h.wantView("⏸ How the Vote Count Works")
}

func TestStatusKeepsTheEpisodeItStarted(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("tab", "down", "enter")...)

	api.playsElsewhere("spotify:show:s0", "show", episodeURI("Inside the Final Hours"), "Inside the Final Hours")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "0:00 / 1:00:00")
}

func TestStatusFollowsTheNextSong(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("enter"))
	h.wantView("⏸ Rock")

	api.playsElsewhere("spotify:playlist:p0", "", "spotify:track:paranoid", "Paranoid")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("⏸ Paranoid")
}

func TestStatusFollowsAnEpisodeOutsideAShow(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	api.playsElsewhere("spotify:show:s1", "episode", episodeURI("Episode 7"), "Episode 7")
	h.send(pollPlayback{})

	h.wantView("⏸ Episode 7")
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
	h.wantView("⏸ Rock")
	h.dontWantView("⏸ So What")

	h.send(pollPlayback{})
	h.wantView("⏸ So What")
}

func TestPollRightAfterAPlayKeepsTheStatus(t *testing.T) {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("enter"))
	h.send(pollPlayback{})

	h.wantView("⏸ Rock")
	h.dontWantView("⏸ So What")
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
	h.wantView("⏸ Episode 1")
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
	h.dontWantView("⏸ Episode 1")
}

func TestNothingPlayingRightAfterAPlayKeepsTheStatus(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(key("enter"))

	api.set(func(f *fakeAPI) { f.paused = true })
	api.playsElsewhere("spotify:show:s1", "show", episodeURI("Episode 1"), "Episode 1")
	h.send(pollPlayback{})
	api.playsElsewhere("", "", "", "")
	h.send(pollPlayback{})

	h.wantView("⏸ How the Vote Count Works")
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

func (h *harness) recordProgressTicks() *[]time.Duration {
	ticks := &[]time.Duration{}
	m := h.player()
	m.progressEvery = time.Second
	m.after = func(d time.Duration, msg tea.Msg) tea.Cmd {
		if _, ok := msg.(progressTicked); ok {
			*ticks = append(*ticks, d)
			return nil
		}
		return testAfter(d, msg)
	}
	h.model = m
	return ticks
}

func (h *harness) agePlaylistStatus(d time.Duration) {
	m := h.player()
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.playback.track.at = playlists.playback.track.at.Add(-d)
	m.tabs[playlistsTab].content = playlists
	h.model = m
}

func bar(filled int) string {
	return strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)
}

func TestTrackLine(t *testing.T) {
	now := time.Now()
	jasmine := track{info: media.Info{Title: "Jasmine", Artist: "Arlo Parks", Duration: 219 * time.Second, Elapsed: 193 * time.Second}, at: now}
	playing := jasmine
	playing.info.Playing, playing.at = true, now.Add(-6*time.Second)
	spaces := jasmine
	spaces.info.Title, spaces.info.Artist = " Jasmine ", "Arlo Parks  "

	for _, tt := range []struct {
		track track
		width int
		want  string
	}{
		{playing, 80, "🎧 ⏸ Jasmine — Arlo Parks " + bar(9) + " 3:19 / 3:39 🎧"},
		{jasmine, 80, "🎧 ▶ Jasmine — Arlo Parks " + bar(8) + " 3:13 / 3:39 🎧"},
		{spaces, 80, "🎧 ▶ Jasmine — Arlo Parks " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 51, "🎧 ▶ Jasmine — Arlo Parks " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 50, "🎧 ▶ Jasmine — Arlo Par… " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 45, "🎧 ▶ Jasmine — Arl… " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 43, "🎧 ▶ Jasmine " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 41, "🎧 ▶ Jasmine " + bar(8) + " 3:13 / 3:39 🎧"},
		{jasmine, 40, "▶ Jasmine — Arlo… " + bar(8) + " 3:13 / 3:39"},
		{jasmine, 34, "▶ Jasmine — Arlo Parks 3:13 / 3:39"},
		{jasmine, 30, "▶ Jasmine — Arlo… 3:13 / 3:39"},
		{jasmine, 23, "▶ Jasmine — Arlo Parks"},
		{jasmine, 20, "▶ Jasmine — Arlo Pa…"},
		{jasmine, 12, "▶ Jasmine"},
		{jasmine, 8, "▶ Jasmi…"},
		{track{info: media.Info{Title: "Rock", Playing: true}, at: now}, 80, "🎧 ⏸ Rock 🎧"},
		{track{info: media.Info{Playing: true}, at: now}, 80, "🎧 ⏸ 🎧"},
		{track{info: media.Info{Artist: "Morning Briefing"}, at: now}, 80, "🎧 ▶ Morning Briefing 🎧"},
	} {
		if got := ansi.Strip(tt.track.line(tt.width, now)); got != tt.want {
			t.Errorf("line(%d) of %+v = %q, want %q", tt.width, tt.track.info, got, tt.want)
		}
	}

	for width := range 80 {
		if got := lipgloss.Width(jasmine.line(width, now)); got > width {
			t.Fatalf("line(%d) is %d columns wide", width, got)
		}
	}
}

func TestTrackLineNeverEndsTheTitleOnTheSeparator(t *testing.T) {
	now := time.Now()
	badCut := regexp.MustCompile(`[\s\p{Pd}]…`)

	for _, info := range []media.Info{
		{Title: "Jasmine", Artist: "Arlo Parks"},
		{Title: "Paranoid - 2012 Remaster", Artist: "Black Sabbath"},
		{Title: "Wish You Were Here – 2011 Remaster", Artist: "Pink Floyd"},
		{Title: "日本語のうた", Artist: "歌手"},
		{Title: "🧑🏿‍🎤🧑🏿‍🎤🧑🏿‍🎤 Song", Artist: "Banda 🧑🏿‍🎤"},
		{Title: "Jasmine", Artist: "———— X"},
	} {
		info.Duration = 219 * time.Second
		for width := range 70 {
			got := ansi.Strip(track{info: info, at: now}.line(width, now))
			if badCut.MatchString(got) {
				t.Errorf("line(%d) of %q = %q cuts the title next to a separator", width, info.Title, got)
			}
		}
	}
}

func TestClip(t *testing.T) {
	for _, tt := range []struct {
		s     string
		width int
		want  string
	}{
		{"Arlo Parks", 10, "Arlo Parks"},
		{"Arlo Parks", 9, "Arlo Par…"},
		{"Arlo Parks", 6, "Arlo…"},
		{"Paranoid - 2012 Remaster", 11, "Paranoid…"},
		{"Here – 2011", 7, "Here…"},
		{"日本語", 2, ""},
		{"日本語", 3, "日…"},
		{"———— X", 4, ""},
		{"Arlo", 1, ""},
		{"Arlo", 0, ""},
	} {
		if got := clip(tt.s, tt.width); got != tt.want {
			t.Errorf("clip(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	for _, tt := range []struct {
		elapsed, duration time.Duration
		filled            int
	}{
		{0, time.Minute, 0},
		{5999 * time.Millisecond, time.Minute, 0},
		{6 * time.Second, time.Minute, 1},
		{30 * time.Second, time.Minute, 5},
		{59 * time.Second, time.Minute, 9},
		{time.Minute, time.Minute, 10},
		{2 * time.Minute, time.Minute, 10},
		{3 * time.Hour, 5 * time.Hour, 6},
	} {
		if got, want := ansi.Strip(progressBar(tt.elapsed, tt.duration)), bar(tt.filled); got != want {
			t.Errorf("progressBar(%v, %v) = %q, want %q", tt.elapsed, tt.duration, got, want)
		}
	}
}

func TestProgressBarColors(t *testing.T) {
	for _, tt := range []struct {
		elapsed time.Duration
		want    string
	}{
		{30 * time.Second, green("█████") + progressEmptyStyle.Render("░░░░░")},
		{time.Minute, green("██████████")},
		{0, progressEmptyStyle.Render("░░░░░░░░░░")},
	} {
		if got := progressBar(tt.elapsed, time.Minute); got != tt.want {
			t.Errorf("progressBar(%v) = %q, want %q", tt.elapsed, got, tt.want)
		}
	}
}

func TestProgressBarStaysReadableWithFewColors(t *testing.T) {
	filled := styles.LipglossColorForegroundGreenSpotify
	for _, profile := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.ANSI} {
		if profile.Convert(styles.LipglossColorDarkGreenShade) == profile.Convert(filled) {
			t.Errorf("%v: the empty cells look like the filled ones", profile)
		}
	}
}

func TestNextProgressTick(t *testing.T) {
	now := time.Now()
	status := func(elapsed, duration time.Duration, playing bool) playbackStatus {
		return playingStatus(0, "", media.Info{Title: "So What", Elapsed: elapsed, Duration: duration, Playing: playing}, now)
	}

	for _, tt := range []struct {
		status playbackStatus
		want   time.Duration
		ok     bool
	}{
		{status(1500*time.Millisecond, time.Minute, true), 500 * time.Millisecond, true},
		{status(2*time.Second, time.Minute, true), time.Second, true},
		{status(time.Minute, time.Minute, true), 0, false},
		{status(time.Second, 0, true), 0, false},
		{status(time.Second, time.Minute, false), 0, false},
		{playbackStatus{text: "Starting Rock...", starting: true}, 0, false},
		{playbackStatus{}, 0, false},
	} {
		got, ok := tt.status.nextTick(now, time.Second)
		if got != tt.want || ok != tt.ok {
			t.Errorf("nextTick(%+v) = %v, %t, want %v, %t", tt.status, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPlayingASongShowsItsArtistAndLength(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "down", "enter")...)

	h.wantView("🎧 ⏸ Paranoid — Black Sabbath " + bar(0) + " 0:00 / 2:48 🎧")
}

func TestStatusShowsAPauseMadeElsewhere(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.wantView("🎧 ⏸ So What — Miles Davis " + bar(1) + " 1:00 / 9:22 🎧")

	api.set(func(f *fakeAPI) { f.paused, f.progressMs = true, 75000 })
	h.send(pollPlayback{})
	h.wantView("🎧 ▶ So What — Miles Davis " + bar(1) + " 1:15 / 9:22 🎧")

	api.set(func(f *fakeAPI) { f.progressMs = 300000 })
	h.send(pollPlayback{})
	h.wantView("🎧 ▶ So What — Miles Davis " + bar(5) + " 5:00 / 9:22 🎧")
}

func TestStatusShowsASeekElsewhereAfterAPauseFromSpotGo(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("k"))
	h.wantView("▶ So What — Miles Davis", "1:00 / 9:22")

	api.set(func(f *fakeAPI) { f.progressMs = 300000 })
	h.settle()
	h.send(pollPlayback{})

	h.wantView("▶ So What — Miles Davis", "5:00 / 9:22")
	if got := h.player().nowPlaying.info; got.Playing || got.Elapsed != 5*time.Minute {
		t.Fatalf("now playing = %+v, want paused at 5:00", got)
	}
}

func TestStatusFreezesWhenNothingPlaysElsewhere(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.agePlaylistStatus(5 * time.Second)

	api.playsElsewhere("", "", "", "")
	h.send(pollPlayback{})
	h.wantView("▶ So What — Miles Davis", "1:05 / 9:22")

	h.agePlaylistStatus(time.Hour)
	h.send(pollPlayback{})
	h.wantView("▶ So What — Miles Davis", "1:05 / 9:22")
}

func TestStatusClampsTheFrozenClockAtTheEnd(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.agePlaylistStatus(time.Hour)

	api.playsElsewhere("", "", "", "")
	h.send(pollPlayback{})

	h.wantView("▶ So What — Miles Davis", "9:22 / 9:22")
}

func TestStatusKeepsPlayingWhenSpotifyOmitsTheItem(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	ticks := h.recordProgressTicks()
	h.send(pollPlayback{})

	api.playsElsewhere("spotify:playlist:p1", "", "", "")
	h.send(pollPlayback{}, progressTicked{})

	h.wantView("⏸ So What — Miles Davis", "1:00 / 9:22")
	if got := h.player().nowPlaying.info; !got.Playing {
		t.Fatalf("now playing = %+v, want still playing", got)
	}
	if len(*ticks) != 2 {
		t.Fatalf("ticks = %v, want the clock to keep ticking", *ticks)
	}
}

func TestStatusKeepsItsClockWhenAPollIsClose(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.agePlaylistStatus(5 * time.Second)

	api.set(func(f *fakeAPI) { f.progressMs = 64400 })
	h.send(pollPlayback{})
	h.wantView("1:05 / 9:22")

	api.set(func(f *fakeAPI) { f.progressMs = 300000 })
	h.send(pollPlayback{})
	h.wantView("5:00 / 9:22")
}

func TestStatusKeepsTheLengthWhenSpotifyOmitsIt(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("tab", "down", "enter")...)

	api.playsElsewhere("spotify:playlist:p0", "", albumTrackURI("Paranoid"), "Paranoid")
	h.settle()
	h.send(pollPlayback{})

	h.wantView("⏸ Paranoid — Black Sabbath", "0:00 / 2:48")
}

func TestProgressTicksOnTheSecondWhilePlaying(t *testing.T) {
	api := soWhatOnPhone()
	api.progressMs = 60400
	h := newHarnessWith(t, api, 120, 40)
	ticks := h.recordProgressTicks()

	h.send(pollPlayback{})
	if len(*ticks) != 1 || (*ticks)[0] <= 500*time.Millisecond || (*ticks)[0] > 600*time.Millisecond {
		t.Fatalf("ticks = %v, want one in the 0.6 second left to 1:01", *ticks)
	}

	h.send(key("down"))
	if len(*ticks) != 1 {
		t.Fatalf("ticks = %v, want no second tick while one is pending", *ticks)
	}

	h.send(progressTicked{})
	if len(*ticks) != 2 || (*ticks)[1] <= 0 || (*ticks)[1] > time.Second {
		t.Fatalf("ticks = %v, want the next one within a second", *ticks)
	}
}

func TestProgressStopsTickingWhilePaused(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	ticks := h.recordProgressTicks()
	h.send(pollPlayback{})

	h.send(key("k"))
	h.wantView("▶ So What")
	h.send(progressTicked{})

	if len(*ticks) != 1 {
		t.Fatalf("ticks = %v, want none after the pause", *ticks)
	}

	h.send(key("k"))
	h.wantView("⏸ So What")
	if len(*ticks) != 2 {
		t.Fatalf("ticks = %v, want ticks again after the resume", *ticks)
	}
}

func TestProgressDoesNotTickWithoutALength(t *testing.T) {
	h := newHarness(t, 120, 40)
	ticks := h.recordProgressTicks()

	h.send(key("enter"))

	h.wantView("🎧 ⏸ Rock 🎧")
	if len(*ticks) != 0 {
		t.Fatalf("ticks = %v, want none without a length to count", *ticks)
	}
}

func TestProgressTicksOnEveryTab(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	ticks := h.recordProgressTicks()
	h.send(pollPlayback{})
	if len(*ticks) != playlistsTab {
		t.Fatalf("ticks = %v, want one on the Playlists tab", *ticks)
	}

	for tab := podcastsTab; tab <= albumsTab; tab++ {
		h.send(key("right"), progressTicked{})
		if got := h.player().activeTab; got != tab {
			t.Fatalf("activeTab = %d, want %d", got, tab)
		}
		if len(*ticks) != tab {
			t.Fatalf("ticks = %v on tab %d, want %d", *ticks, tab, tab)
		}
	}

	h.send(keys("left", "left", "left", "left")...)
	h.send(progressTicked{})
	if got := h.player().activeTab; got != searchTab {
		t.Fatalf("activeTab = %d, want the Search tab", got)
	}
	if len(*ticks) != albumsTab+1 {
		t.Fatalf("ticks = %v, want one on the Search tab too", *ticks)
	}
}
func TestNowPlayingLineFitsEveryWidth(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = []string{songJSON(strings.Repeat("Uma música com um nome bem comprido 👋🏽 ", 3), "Banda 🧑🏿‍🎤 com nome longo", 219000)}
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(keys("tab", "enter")...)
	h.send(volumeChanged{percent: 50})

	for _, volume := range []bool{true, false} {
		if !volume {
			h.send(volumeHidden{seq: h.player().volume.seq})
		}
		for width := 60; width <= 160; width++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: 24})
			v := h.view()
			if !strings.Contains(v, "⏸ Uma") {
				t.Fatalf("%dx24: status is missing:\n%s", width, v)
			}
			for i, line := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(line); got != width {
					t.Fatalf("%dx24 volume %t: line %d is %d columns wide, want %d:\n%s", width, volume, i, got, width, v)
				}
			}
		}
	}
}

func TestPauseWhileSpotifyOmitsTheItemKeepsTheSong(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)
	published := h.recordNowPlaying()
	h.send(pollPlayback{})

	api.playsElsewhere("spotify:playlist:p1", "", "", "")
	h.send(key("k"))

	h.wantPauses("")
	h.wantView("🎧 ▶ So What — Miles Davis " + bar(1) + " 1:00 / 9:22 🎧")
	if got := lastInfo(t, published); got.Title != "So What" || got.Playing {
		t.Fatalf("now playing = %+v, want So What paused", got)
	}
}

func TestSkipKeepsTheSongWhileSpotifyOmitsTheNewOne(t *testing.T) {
	api := soWhatOnPhone()
	api.playLag = 1
	api.started = func(f *fakeAPI) { f.playingItem, f.playingName = "", "" }
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(media.NextTrack)

	h.wantSkips("next")
	h.wantPlayerReads(1 + confirmPolls)
	h.wantView("⏸ So What — Miles Davis")
	if got := lastInfo(t, published); got.Title != "So What" || !got.Playing {
		t.Fatalf("now playing = %+v, want So What playing", got)
	}
}
