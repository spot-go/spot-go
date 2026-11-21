package player

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/franciscocpg/spot-go/internal/library"
	"github.com/franciscocpg/spot-go/internal/spotify"
)

type testEpisode struct {
	name        string
	date        string
	precision   string
	durationMs  int
	fullyPlayed bool
	resumeMs    int
	unplayable  bool
}

var testShows = []struct{ id, name, added string }{
	{"s0", "Morning Briefing", "2026-10-01T10:00:00Z"},
	{"s1", "Go Weekly", "2026-09-01T10:00:00Z"},
	{"s2", "History Hour", "2025-01-01T10:00:00Z"},
}

func showJSON(id, name, added string) string {
	return fmt.Sprintf(`{"added_at":%q,"show":{"id":%q,"uri":"spotify:show:%s","name":%q}}`, added, id, id, name)
}

func testShowsJSON() []string {
	shows := make([]string, len(testShows))
	for i, s := range testShows {
		shows[i] = showJSON(s.id, s.name, s.added)
	}
	return shows
}

func episodeID(name string) string {
	return fmt.Sprintf("%x", name)
}

func episodeURI(name string) string {
	return "spotify:episode:" + episodeID(name)
}

func episodeJSON(e testEpisode) string {
	playable := ""
	if e.unplayable {
		playable = `,"is_playable":false`
	}
	return fmt.Sprintf(`{"id":%q,"uri":%q,"name":%q,"duration_ms":%d,"release_date":%q,"release_date_precision":%q,"resume_point":{"fully_played":%t,"resume_position_ms":%d}%s}`,
		episodeID(e.name), episodeURI(e.name), e.name, e.durationMs, e.date, e.precision, e.fullyPlayed, e.resumeMs, playable)
}

func manyEpisodesJSON(n int) []string {
	episodes := make([]string, n)
	for i := range episodes {
		episodes[i] = episodeJSON(testEpisode{name: fmt.Sprintf("Episode %d", i+1), date: "2025-06-01", precision: "day", durationMs: 1800000})
	}
	return episodes
}

func (f *fakeAPI) showLoads() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.showRequests
}

func (f *fakeAPI) reads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.episodeReads...)
}

func (h *harness) podcasts() podcastModel {
	return h.player().tabs[podcastsTab].content.(podcastModel)
}

func (h *harness) selectedShow() string {
	s, _ := h.podcasts().list.SelectedItem().(showItem)
	return s.title
}

func (h *harness) showTitles() []string {
	var titles []string
	for _, li := range h.podcasts().list.VisibleItems() {
		titles = append(titles, li.(showItem).title)
	}
	return titles
}

func (h *harness) wantShows(label string, titles ...string) {
	h.t.Helper()

	if got := h.showTitles(); !reflect.DeepEqual(got, titles) {
		h.t.Fatalf("podcasts = %v, want %v", got, titles)
	}
	if got := h.podcasts().list.Title; got != label {
		h.t.Fatalf("list title = %q, want %q", got, label)
	}
	h.wantView(label)
}

func (h *harness) episodeNames() []string {
	h.t.Helper()

	e := h.podcasts().selectedEpisodes()
	if e == nil || e.list == nil {
		h.t.Fatalf("episodes are not loaded:\n%s", h.view())
	}
	var names []string
	for _, ep := range listedEpisodes(e.list) {
		names = append(names, ep.Name)
	}
	return names
}

func (h *harness) selectedEpisode() string {
	ep, _ := h.podcasts().selectedEpisode()
	return ep.Name
}

func (h *harness) wantEpisodePlay(i int, show, episode string, positionMs int) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	got := plays[i]
	want := map[string]any{
		"context_uri": show,
		"offset":      map[string]any{"uri": episodeURI(episode)},
	}
	if positionMs > 0 {
		want["position_ms"] = float64(positionMs)
	}
	if got.deviceID != testDeviceID || !reflect.DeepEqual(got.body, want) {
		h.t.Fatalf("play %d = device %q body %v, want device %q body %v", i, got.deviceID, got.body, testDeviceID, want)
	}
}

func (h *harness) wantPlays(n int) {
	h.t.Helper()

	if got := len(h.api.playRequests()); got != n {
		h.t.Fatalf("plays = %d, want %d", got, n)
	}
}

func newPodcastHarness(t *testing.T, api *fakeAPI) *harness {
	t.Helper()

	h := newHarnessWith(t, api, 120, 40)
	h.send(key("right"))
	return h
}

func TestPodcastsLoadWhenTheTabOpens(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	if got := api.showLoads(); got != 0 {
		t.Fatalf("podcasts were requested %d times before the tab opened", got)
	}

	h.send(key("right"))
	h.wantView("Morning Briefing", "Go Weekly", "History Hour", "3 podcasts")
	h.wantShows("Recents", "Morning Briefing", "Go Weekly", "History Hour")

	h.send(keys("left", "right")...)
	if got := api.showLoads(); got != 1 {
		t.Fatalf("podcasts were requested %d times, want 1", got)
	}
	if h.podcastLoads != 1 {
		t.Fatalf("podcasts loaded %d times, want 1", h.podcastLoads)
	}
	if got, want := api.requestsFor("s0"), []string{"s0@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
}

func TestAllPagesOfPodcastsLoad(t *testing.T) {
	api := newFakeAPI()
	api.shows = nil
	for i := range 70 {
		api.shows = append(api.shows, showJSON(fmt.Sprintf("x%d", i), fmt.Sprintf("Show %d", i), "2026-01-01T00:00:00Z"))
	}
	h := newPodcastHarness(t, api)

	if got := len(h.podcasts().list.Items()); got != 70 {
		t.Fatalf("podcasts = %d, want 70", got)
	}
	if got := api.showLoads(); got != 2 {
		t.Fatalf("podcast pages requested = %d, want 2", got)
	}
}

func TestEpisodesOfTheFirstPodcastLoad(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.wantView("✓ How the Vote Count Works", "Nov 7, 2024", "52:14", "Inside the Final Hours", "Nov 6, 2024", "31m left", "Trailer", "2018", "2:05", "3 episodes")
	if got, want := h.episodeNames(), []string{"How the Vote Count Works", "Inside the Final Hours", "Trailer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episodes = %v, want %v", got, want)
	}
}

func TestNoPodcasts(t *testing.T) {
	api := newFakeAPI()
	api.shows = nil
	h := newPodcastHarness(t, api)

	h.wantView("No podcasts", "You don't follow any podcasts.", "then press r")
	api.set(func(f *fakeAPI) { f.shows = testShowsJSON() })
	h.send(key("r"))

	h.wantShows("Recents", "Morning Briefing", "Go Weekly", "History Hour")
	if h.podcastLoads != 2 {
		t.Fatalf("podcasts loaded %d times, want 2", h.podcastLoads)
	}
}

func TestPodcastsLoadErrorAndRetry(t *testing.T) {
	api := newFakeAPI()
	api.showsStatus = http.StatusInternalServerError
	h := newPodcastHarness(t, api)

	h.wantView("Fail to load podcasts", "request failed", "Press r to try again")
	h.send(keys("down", "enter", "s")...)
	h.wantPlays(0)

	api.set(func(f *fakeAPI) { f.showsStatus = 0 })
	h.send(key("r"))
	h.wantShows("Recents", "Morning Briefing", "Go Weekly", "History Hour")
	if got := api.showLoads(); got != 2 {
		t.Fatalf("podcasts were requested %d times, want 2", got)
	}
}

func TestPodcastsLoadedWhileOnAnotherTab(t *testing.T) {
	h := newHarness(t, 120, 40)
	model, cmd := h.model.Update(key("right"))
	h.model = model

	h.send(key("left"))
	h.run(cmd)
	h.send(key("right"))

	if h.podcasts().state != statePodcastsLoaded {
		t.Fatalf("state = %v, want loaded", h.podcasts().state)
	}
	h.wantView("Morning Briefing")
}

func TestResizeKeepsTheSelectedPodcast(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(key("down"))

	for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	}
	h.send(key("left"))
	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	h.send(key("right"))

	if got := h.selectedShow(); got != "Go Weekly" {
		t.Fatalf("selected = %q, want Go Weekly", got)
	}
	if got, want := tabSize(h.podcasts()), h.player().contentDimension(); got != want {
		t.Fatalf("podcasts tab size = %+v, want %+v", got, want)
	}
	if h.podcastLoads != 1 {
		t.Fatalf("podcasts loaded %d times, want 1", h.podcastLoads)
	}
}

func TestMovingThePodcastCursorWaitsBeforeLoadingEpisodes(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.holdTimers = true

	h.send(key("down"))
	if got := api.requestsFor("s1"); len(got) != 0 {
		t.Fatalf("episodes were requested right after the cursor moved: %v", got)
	}
	h.wantView("Loading episodes...")
	if want := []time.Duration{songsLoadDelay}; !reflect.DeepEqual(h.delays, want) {
		t.Fatalf("delays = %v, want %v", h.delays, want)
	}

	h.holdTimers = false
	h.send()
	if got, want := api.requestsFor("s1"), []string{"s1@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
	h.wantView("Episode 1")
}

func TestFastScrollingLoadsOnlyTheSelectedPodcast(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	h.send(keys("down", "down")...)

	if got := api.requestsFor("s1"); len(got) != 0 {
		t.Fatalf("episode requests for s1 = %v, want none for a podcast scrolled past", got)
	}
	if got, want := api.requestsFor("s2"), []string{"s2@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests for s2 = %v, want %v", got, want)
	}
}

func TestEpisodesAreLoadedOncePerPodcast(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	for _, k := range []string{"down", "up", "down", "up"} {
		h.send(key(k))
	}

	if got := api.requestsFor("s0"); len(got) != 1 {
		t.Fatalf("episode requests for s0 = %v, want 1", got)
	}
	if got := api.requestsFor("s1"); len(got) != 1 {
		t.Fatalf("episode requests for s1 = %v, want 1", got)
	}
}

func TestEpisodesLoadOnePageAtATime(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	h.send(key("down"))
	h.wantView("Go Weekly (50 of 120)", "50 episodes")
	if got, want := api.requestsFor("s1"), []string{"s1@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}

	h.send(key("tab"))
	for range 39 {
		h.send(key("down"))
	}
	if got := api.requestsFor("s1"); len(got) != 1 {
		t.Fatalf("episodes far from the end of the list loaded more pages: %v", got)
	}

	h.send(key("down"))
	h.wantView("Go Weekly (100 of 120)", "100 episodes")
	if got, want := api.requestsFor("s1"), []string{"s1@0", "s1@50"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}

	h.send(key("end"))
	h.wantView("120 episodes")
	h.dontWantView("of 120")
	h.send(keys("end", "up", "down")...)
	if got, want := api.requestsFor("s1"), []string{"s1@0", "s1@50", "s1@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
}

func TestShortPagesLoadUntilTheListIsLongEnough(t *testing.T) {
	api := newFakeAPI()
	api.episodes["s1"] = api.episodes["s1"][:8]
	api.totals["s1"] = 30
	h := newPodcastHarness(t, api)

	h.send(key("down"))

	if got, want := api.requestsFor("s1"), []string{"s1@0", "s1@8"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
	h.wantView("8 episodes")
	h.dontWantView("of 30")
}

func TestDuplicateEpisodesAcrossPagesAreDropped(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("down", "tab")...)

	api.set(func(f *fakeAPI) {
		f.episodes["s1"] = append([]string{episodeJSON(testEpisode{name: "Breaking News", date: "2026-11-08", precision: "day", durationMs: 600000})}, f.episodes["s1"]...)
	})
	for range 40 {
		h.send(key("down"))
	}

	names := h.episodeNames()
	if len(names) != 99 {
		t.Fatalf("episodes = %d, want 99", len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("%q is listed twice", name)
		}
		seen[name] = true
	}
	if seen["Breaking News"] {
		t.Fatal("an episode published after the first page was added in the middle of the list")
	}
}

func TestEmptyPodcastShowsNoEpisodes(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("down", "down")...)

	h.wantView("History Hour", "No episodes")
}

func TestEpisodesLoadErrorAndRetry(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		api := newFakeAPI()
		api.songsStatus["s0"] = status
		h := newPodcastHarness(t, api)

		h.wantView("Fail to load episodes", "request failed", "Press r to try again")
		h.dontWantView("playlists you own")

		api.set(func(f *fakeAPI) { delete(f.songsStatus, "s0") })
		h.send(key("r"))
		h.wantView("How the Vote Count Works")
	}
}

func TestPartialEpisodeFailureStopsLoadingMore(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["s1"] = rateLimit{offset: "50", times: 10, retryAfter: "3600"}
	h := newPodcastHarness(t, api)
	h.send(keys("down", "tab")...)

	for range 40 {
		h.send(key("down"))
	}
	h.wantView("Go Weekly (fail to load: API rate limit exceeded)", "Episode 41")
	h.send(keys("down", "down", "end")...)
	if got, want := api.requestsFor("s1"), []string{"s1@0", "s1@50"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests after a failed page = %v, want %v", got, want)
	}

	api.set(func(f *fakeAPI) { delete(f.rateLimits, "s1") })
	h.send(key("r"))
	if got := len(h.episodeNames()); got < 100 {
		t.Fatalf("episodes after refreshing = %d, want at least 100", got)
	}
	h.dontWantView("fail to load")
}

func TestRefreshReloadsTheLoadedEpisodes(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("down", "tab")...)
	for range 40 {
		h.send(key("down"))
	}
	if got := h.selectedEpisode(); got != "Episode 41" {
		t.Fatalf("selected = %q, want Episode 41", got)
	}

	api.set(func(f *fakeAPI) {
		f.episodes["s1"] = append([]string{episodeJSON(testEpisode{name: "Breaking News", date: "2026-11-08", precision: "day", durationMs: 600000})}, f.episodes["s1"]...)
	})
	h.send(key("r"))

	if got, want := api.requestsFor("s1"), []string{"s1@0", "s1@50", "s1@0", "s1@50"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
	if got := h.episodeNames(); len(got) != 100 || got[0] != "Breaking News" {
		t.Fatalf("episodes after refreshing = %d starting with %q, want 100 starting with Breaking News", len(got), got[0])
	}
	if got := h.selectedEpisode(); got != "Episode 41" {
		t.Fatalf("selected after refreshing = %q, want Episode 41", got)
	}
}

func TestRefreshKeepsTheEpisodesWhileLoading(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	p := h.podcasts()
	first := p.refreshEpisodes("s0")
	h.wantView("Morning Briefing (refreshing...)", "How the Vote Count Works")

	second := p.refreshEpisodes("s0")
	h.run(first)
	h.wantView("Morning Briefing (refreshing...)")

	h.run(second)
	h.dontWantView("refreshing...")
	if got := len(h.episodeNames()); got != 3 {
		t.Fatalf("episodes = %d, want 3", got)
	}
}

func TestFailedRefreshKeepsTheEpisodes(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	api.set(func(f *fakeAPI) { f.songsStatus["s0"] = http.StatusInternalServerError })
	h.send(key("r"))

	h.wantView("Morning Briefing (fail to load: request failed)", "How the Vote Count Works")
	if got := len(h.episodeNames()); got != 3 {
		t.Fatalf("episodes = %d, want 3", got)
	}
}

func TestRefreshRestartsAStalledEpisodeLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["s1@50"] = release
	h := newPodcastHarness(t, api)
	h.send(keys("down", "tab")...)
	for range 40 {
		h.send(key("down"))
	}
	h.wantView("Go Weekly (loading 50 of 120...)")

	h.send(key("r"))

	if got := len(h.episodeNames()); got != 100 {
		t.Fatalf("episodes = %d, want 100", got)
	}
	h.dontWantView("loading", "refreshing")
}

func TestStaleEpisodeResultsAreIgnored(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	p := h.podcasts()
	old := p.episodes["s0"].gen
	refresh := p.refreshEpisodes("s0")

	stale := spotify.EpisodesPage{Items: []spotify.Episode{{Name: "Old News", URI: "spotify:episode:old"}}, Next: 1, Total: 1, Last: true}
	h.send(episodesLoadFailed{showID: "s0", gen: old, err: errors.New("timeout")})
	h.send(episodesPageLoaded{showID: "s0", gen: old, page: stale})
	h.wantView("Morning Briefing (refreshing...)", "How the Vote Count Works")
	h.dontWantView("Old News", "timeout")

	h.run(refresh)
	h.dontWantView("refreshing", "fail to load", "Old News")
	if got := len(h.episodeNames()); got != 3 {
		t.Fatalf("episodes = %d, want 3", got)
	}
}

func TestRefreshingTwiceKeepsTheSelectedEpisode(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.send(keys("down", "tab")...)
	for range 80 {
		h.send(key("down"))
	}
	if got := h.selectedEpisode(); got != "Episode 81" {
		t.Fatalf("selected = %q, want Episode 81", got)
	}

	api.set(func(f *fakeAPI) { f.rateLimits["s1"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"} })
	h.send(key("r"))
	h.wantView("Go Weekly (fail to load: API rate limit exceeded)")
	h.send(key("r"))

	if got := len(h.episodeNames()); got != 100 {
		t.Fatalf("episodes = %d, want 100", got)
	}
	if got := h.selectedEpisode(); got != "Episode 81" {
		t.Fatalf("selected after refreshing twice = %q, want Episode 81", got)
	}
}

func TestEpisodeRowsFillTheWidth(t *testing.T) {
	episodes := []spotify.Episode{
		{Name: "A very long episode name that never fits in a narrow terminal 🎙️", Duration: 3134 * time.Second, Released: time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC), Precision: "day", Playable: true, FullyPlayed: true},
		{Name: "Short", Duration: 2*time.Hour + 15*time.Minute, Resume: time.Minute, Released: time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC), Precision: "month", Playable: true},
		{Name: "Gone", Duration: time.Minute, Playable: false},
		{Name: "Marathon", Duration: 12 * time.Hour, Resume: time.Hour + 50*time.Minute, Playable: true},
	}
	for _, focused := range []bool{false, true} {
		for width := 16; width <= 120; width++ {
			l := newList(episodeItems(episodes), episodeDelegate{focused: focused})
			l.SetSize(width, 10)
			for i, li := range l.Items() {
				var b bytes.Buffer
				episodeDelegate{focused: focused}.Render(&b, l, i, li)
				row := b.String()
				if got := ansi.StringWidth(row); got != width {
					t.Fatalf("width %d: row %d is %d columns wide: %q", width, i, got, ansi.Strip(row))
				}
				if i == 3 && width >= 2+2+1+episodeTimeWidth && !strings.Contains(row, "10h 10m left") {
					t.Fatalf("width %d: the time left of a long episode is cut: %q", width, ansi.Strip(row))
				}
				hasDate := strings.Contains(row, "Dec 30, 2025") || strings.Contains(row, "Mar 2019")
				wantDate := i < 2 && width-2-2-1-episodeTimeWidth-episodeDateWidth-2 >= minEpisodeTitleWidth
				if hasDate != wantDate {
					t.Fatalf("width %d: row %d shows the date = %v, want %v: %q", width, i, hasDate, wantDate, ansi.Strip(row))
				}
			}
		}
	}
}

func TestReleaseLabel(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		released  time.Time
		precision string
		want      string
	}{
		{time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), "day", "Nov 7"},
		{time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC), "day", "Dec 30, 2025"},
		{time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC), "month", "Mar 2019"},
		{time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), "year", "2018"},
		{time.Time{}, "", ""},
	} {
		if got := releaseLabel(spotify.Episode{Released: tt.released, Precision: tt.precision}, now); got != tt.want {
			t.Errorf("releaseLabel(%v, %s) = %q, want %q", tt.released, tt.precision, got, tt.want)
		}
	}
}

func TestRemainingLabel(t *testing.T) {
	for _, tt := range []struct {
		left time.Duration
		want string
	}{
		{10 * time.Second, "1m left"},
		{time.Minute, "1m left"},
		{31 * time.Minute, "31m left"},
		{30*time.Minute + time.Second, "31m left"},
		{59*time.Minute + 30*time.Second, "1h left"},
		{2 * time.Hour, "2h left"},
		{2*time.Hour + 14*time.Minute + time.Second, "2h 15m left"},
	} {
		if got := remainingLabel(tt.left); got != tt.want {
			t.Errorf("remainingLabel(%v) = %q, want %q", tt.left, got, tt.want)
		}
	}
}

func TestEpisodeTimeShowsWhatIsLeftOfStartedEpisodes(t *testing.T) {
	for _, tt := range []struct {
		episode spotify.Episode
		want    string
	}{
		{spotify.Episode{Duration: 52*time.Minute + 14*time.Second}, "52:14"},
		{spotify.Episode{Duration: time.Hour, Resume: 29 * time.Minute}, "31m left"},
		{spotify.Episode{Duration: time.Hour, Resume: 29 * time.Minute, FullyPlayed: true}, "1:00:00"},
		{spotify.Episode{Duration: time.Hour, Resume: time.Hour}, "1:00:00"},
	} {
		if got := episodeTime(tt.episode); got != tt.want {
			t.Errorf("episodeTime(%+v) = %q, want %q", tt.episode, got, tt.want)
		}
	}
}

func TestPodcastsViewFitsTheWindow(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(keys("down", "tab")...)

	for _, width := range []int{10, 30, 56, 57, 61, 80, 120, 200} {
		for height := 1; height <= 45; height++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: height})
			v := h.view()
			if got := lipgloss.Height(v); got > height {
				t.Fatalf("%dx%d: view has %d lines", width, height, got)
			}
			if got := lipgloss.Width(v); got > width {
				t.Fatalf("%dx%d: view is %d columns wide", width, height, got)
			}
		}
	}

	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.wantView("Go Weekly", "Episode 1")
}

func TestPodcastsFrameReachesTheRightEdge(t *testing.T) {
	api := newFakeAPI()
	api.shows = append(api.shows, showJSON("s3", "👩🏾‍❤️‍👩🏼 casal 🇧🇷 com um nome bem comprido 👋🏿", "2024-01-01T00:00:00Z"))
	api.episodes["s0"] = append(api.episodes["s0"], episodeJSON(testEpisode{name: "Tchau 👋🏽 e um título bem comprido", date: "2026-11-01", precision: "day", durationMs: 100000}))
	h := newPodcastHarness(t, api)
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, focus := range []string{"", "tab"} {
		if focus != "" {
			h.send(key(focus))
		}
		for width := minWidth; width <= 200; width++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: 24})
			v := h.view()
			if strings.Contains(v, tooSmallWindow) {
				t.Fatalf("%dx24 shows the too small message", width)
			}
			for i, line := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(line); got != width {
					t.Fatalf("%dx24: line %d is %d columns wide, want %d:\n%s", width, i, got, width, v)
				}
			}
		}
	}
}

func TestPodcastOrderLabelStaysOnTheFirstRow(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, label := range []string{"Recently added", "Alphabetical", "Recents"} {
		h.send(key("s"))
		for width := minWidth; width <= 160; width++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: 24})
			lines := strings.Split(h.view(), "\n")
			first, second := strings.Split(lines[3], "│")[1], strings.Split(lines[4], "│")[1]
			if !strings.Contains(first, label[:4]) || strings.TrimSpace(second) != "" {
				t.Fatalf("%s at %dx24: the label does not fit on the first row of the pane:\n%s", label, width, h.view())
			}
		}
	}
}

func TestEpisodesListFollowsTheWindowSize(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(key("down"))

	for _, size := range [][2]int{{120, 40}, {80, 24}, {200, 60}, {60, 10}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		p := h.podcasts()
		l := p.selectedEpisodes().list
		if l.Width() != p.rightWidth() || l.Height() != p.listHeight() {
			t.Fatalf("%dx%d: episodes list is %dx%d, want %dx%d", size[0], size[1], l.Width(), l.Height(), p.rightWidth(), p.listHeight())
		}
	}
}

func TestPodcastsViewDoesNotStartGoroutines(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	m := h.player()

	before := runtime.NumGoroutine()
	for range 200 {
		_ = m.View()
	}
	if after := runtime.NumGoroutine(); after-before > 20 {
		t.Fatalf("View started goroutines: %d before, %d after", before, after)
	}
}

func TestEnterPlaysTheSelectedEpisode(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "Inside the Final Hours", 1740000)
	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "29:00 / 1:00:00")
	if got, want := api.reads(), []string{episodeID("Inside the Final Hours")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode reads = %v, want %v", got, want)
	}
}

func TestEnterResumesFromTheLatestPosition(t *testing.T) {
	api := newFakeAPI()
	api.fresh[episodeID("Inside the Final Hours")] = episodeJSON(testEpisode{name: "Inside the Final Hours", date: "2024-11-06", precision: "day", durationMs: 3600000, resumeMs: 2400000})
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "Inside the Final Hours", 2400000)
	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "40:00 / 1:00:00")
}

func TestEpisodeFinishedElsewherePlaysFromTheStart(t *testing.T) {
	api := newFakeAPI()
	api.fresh[episodeID("Inside the Final Hours")] = episodeJSON(testEpisode{name: "Inside the Final Hours", date: "2024-11-06", precision: "day", durationMs: 3600000, resumeMs: 3600000, fullyPlayed: true})
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "Inside the Final Hours", 0)
}

func TestEnterUsesTheListedPositionWhenTheEpisodeReadIsEmpty(t *testing.T) {
	api := newFakeAPI()
	api.fresh[episodeID("Inside the Final Hours")] = `{}`
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "Inside the Final Hours", 1740000)
	h.wantView("⏸ Inside the Final Hours — Morning Briefing", "29:00 / 1:00:00")
}

func TestEnterUsesTheListedPositionWhenTheEpisodeCantBeRead(t *testing.T) {
	api := newFakeAPI()
	api.episodeStatus = http.StatusInternalServerError
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "Inside the Final Hours", 1740000)
}

func TestFinishedEpisodePlaysFromTheStart(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("tab", "enter")...)

	h.wantEpisodePlay(0, "spotify:show:s0", "How the Vote Count Works", 0)
	h.wantView("⏸ How the Vote Count Works — Morning Briefing", "0:00 / 52:14")
}

func TestEnterOnAPodcastPlaysTheNewestEpisode(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(key("enter"))

	h.wantEpisodePlay(0, "spotify:show:s0", "How the Vote Count Works", 0)
}

func TestEnterOnAPodcastSkipsUnplayableEpisodes(t *testing.T) {
	api := newFakeAPI()
	api.episodes["s0"] = append([]string{episodeJSON(testEpisode{name: "Members Only", date: "2024-11-08", precision: "day", durationMs: 600000, unplayable: true})}, api.episodes["s0"]...)
	h := newPodcastHarness(t, api)

	h.send(key("enter"))

	h.wantEpisodePlay(0, "spotify:show:s0", "How the Vote Count Works", 0)
}

func TestEnterOnAPodcastBeforeItsEpisodesLoad(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)
	h.holdTimers = true

	h.send(key("down"))
	h.wantView("Loading episodes...")
	h.send(key("enter"))

	h.wantEpisodePlay(0, "spotify:show:s1", "Episode 1", 0)
	h.holdTimers = false
	h.send()
	if got, want := api.requestsFor("s1"), []string{"s1@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("episode requests = %v, want %v", got, want)
	}
	h.wantPlays(1)
}

func TestPendingPodcastPlayShowsWhatItWaitsFor(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.holdTimers = true
	h.send(key("down"))

	p, cmd := h.podcasts().play()
	if cmd == nil || p.playback.text != "Starting Go Weekly..." || p.pending.show != "s1" {
		t.Fatalf("play before the episodes load = %+v pending %+v", p.playback, p.pending)
	}
}

func TestPendingPodcastPlayFailsWithTheLoad(t *testing.T) {
	api := newFakeAPI()
	api.songsStatus["s1"] = http.StatusInternalServerError
	h := newPodcastHarness(t, api)
	h.holdTimers = true

	h.send(keys("down", "enter")...)

	h.wantView("Fail to play: request failed", "Fail to load episodes")
	h.wantPlays(0)
}

func TestLaterPlayCancelsThePendingPodcastPlay(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.holdTimers = true
	h.send(key("down"))

	p, load := h.podcasts().play()
	p.list.Select(0)
	p, _ = p.play()
	m := h.player()
	m.tabs[podcastsTab].content = p
	h.model = m
	h.wantPlays(0)

	h.run(load)
	h.wantPlays(0)
	if got := h.podcasts().pending; got != (pendingPlay{}) {
		t.Fatalf("pending = %+v, want none", got)
	}
}

func TestPlayingAPlaylistCancelsThePendingPodcastPlay(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.holdTimers = true
	h.send(key("down"))

	model, load := h.model.Update(key("enter"))
	h.model = model
	h.send(key("left"))
	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p0", 0)

	h.run(load)
	h.wantPlays(1)
	if got := h.podcasts().pending; got != (pendingPlay{}) {
		t.Fatalf("pending = %+v, want none", got)
	}
}

func TestPodcastWithoutEpisodesSaysSo(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("down", "down")...)
	h.holdTimers = false
	h.send(key("enter"))

	h.wantView("History Hour has no episodes to play")
	h.wantPlays(0)
}

func TestUnplayableEpisodeIsNotSentToSpotify(t *testing.T) {
	api := newFakeAPI()
	h := newPodcastHarness(t, api)

	h.send(keys("tab", "down", "down", "enter")...)

	h.wantView("Trailer is unavailable")
	h.wantPlays(0)
	if got := api.reads(); len(got) != 0 {
		t.Fatalf("episode reads = %v, want none", got)
	}
}

func TestEpisodePlaybackErrorIsShown(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newPodcastHarness(t, api)

	h.send(key("enter"))

	h.wantView("Fail to play", "Premium required")
}

func TestStaleEpisodePlaybackResultIsIgnored(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(keys("tab", "enter")...)

	h.send(episodePlaybackFailed{seq: 0, err: errors.New("old failure")})
	h.send(episodePlaybackStarted{seq: 0, show: "spotify:show:s0", name: "Old Episode"})

	h.wantView("⏸ How the Vote Count Works")
	h.dontWantView("old failure", "Old Episode")
}

func TestEachTabKeepsItsOwnPlaybackStatus(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(key("enter"))
	h.send(playbackStarted{seq: h.podcasts().playback.seq, name: "Rock", uri: "spotify:playlist:p0"})
	h.wantView("⏸ How the Vote Count Works")

	h.send(key("left"))
	h.dontWantView("How the Vote Count Works")
}

func TestPodcastSortKeyCyclesTheOrders(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:s2"}})
	h.wantShows("Recents", "History Hour", "Morning Briefing", "Go Weekly")

	h.send(key("s"))
	h.wantShows("Recently added", "Morning Briefing", "Go Weekly", "History Hour")
	h.send(key("s"))
	h.wantShows("Alphabetical", "Go Weekly", "History Hour", "Morning Briefing")
	h.send(key("s"))
	h.wantShows("Recents", "History Hour", "Morning Briefing", "Go Weekly")
}

func TestPodcastOrdersUseWhenTheShowsWereFollowed(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(showsLoaded{shows: []spotify.SavedShow{
		{ID: "z", URI: "spotify:show:z", Name: "Zebra Talk", AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "a", URI: "spotify:show:a", Name: "Apple Pod", AddedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "m", URI: "spotify:show:m", Name: "Mango Hour", AddedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)},
	}})
	h.wantShows("Recents", "Apple Pod", "Zebra Talk", "Mango Hour")

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:m"}})
	h.wantShows("Recents", "Mango Hour", "Apple Pod", "Zebra Talk")
	h.send(key("s"))
	h.wantShows("Recently added", "Apple Pod", "Zebra Talk", "Mango Hour")
	h.send(key("s"))
	h.wantShows("Alphabetical", "Apple Pod", "Mango Hour", "Zebra Talk")
}

func TestSortShows(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	items := []list.Item{
		showItem{title: "b", uri: "spotify:show:b", addedAt: day(5), index: 0},
		showItem{title: "a", uri: "spotify:show:a", addedAt: day(5), index: 1},
		showItem{title: "c", uri: "spotify:show:c", addedAt: day(9), index: 2},
		showItem{title: "d", uri: "spotify:show:d", addedAt: day(1), index: 3},
	}
	var history library.History
	history.PodcastPlayed("spotify:show:d", day(3))
	history.PodcastPlayed("spotify:show:a", day(7))

	for _, tt := range []struct {
		order library.Order
		want  []string
	}{
		{library.OrderRecents, []string{"c", "a", "b", "d"}},
		{library.OrderRecentlyAdded, []string{"c", "b", "a", "d"}},
		{library.OrderAlphabetical, []string{"a", "b", "c", "d"}},
		{library.OrderCreator, []string{"c", "a", "b", "d"}},
	} {
		var got []string
		for _, li := range sortShows(items, tt.order, history) {
			got = append(got, li.(showItem).title)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("sortShows(%s) = %v, want %v", tt.order, got, tt.want)
		}
	}
}

func TestPodcastSortKeepsTheSelectedPodcast(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(keys("down", "down")...)

	h.send(keys("s", "s")...)

	if got := h.selectedShow(); got != "History Hour" {
		t.Fatalf("selected = %q, want History Hour", got)
	}
}

func TestPlayingAPodcastMovesItToTheTop(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("down", "enter")...)

	h.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
	if got := h.selectedShow(); got != "Go Weekly" {
		t.Fatalf("selected = %q, want Go Weekly", got)
	}
}

func TestPollingRecordsPodcastsPlayedElsewhere(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:show:s2", "show"
	h := newPodcastHarness(t, api)

	h.send(pollPlayback{})
	h.wantShows("Recents", "History Hour", "Morning Briefing", "Go Weekly")

	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:show:s1", "episode" })
	h.send(pollPlayback{})
	h.wantShows("Recents", "Go Weekly", "History Hour", "Morning Briefing")

	api.set(func(f *fakeAPI) {
		if f.playerRequests != 2 {
			t.Fatalf("player requests = %d, want 2", f.playerRequests)
		}
	})
}

func TestPollingBeforeThePodcastsTabOpensIsKept(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:show:s1", "show"
	h := newHarnessWith(t, api, 120, 40)

	h.send(pollPlayback{})
	h.send(key("right"))

	h.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
}

func TestPodcastsTabDoesNotPoll(t *testing.T) {
	api := newFakeAPI()
	m := newModelWith(api, 120, 40)
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.pollEvery = time.Minute
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m, holdTimers: true}
	h.run(m.Init())
	h.send(key("right"))
	h.timers = nil

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:s0"}})

	if len(h.timers) != 1 {
		t.Fatalf("timers after one poll = %v, want one", h.timers)
	}
	if _, ok := h.timers[0].(pollPlayback); !ok {
		t.Fatalf("timer = %#v, want the next poll", h.timers[0])
	}
}

func TestPodcastOrderAndHistoryAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))
	h.send(key("right"))

	h.send(keys("down", "down", "s", "enter")...)
	h.wantShows("Recently added", "Morning Briefing", "Go Weekly", "History Hour")
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:s1"}})

	var saved struct {
		Podcasts struct {
			Order      string               `json:"order"`
			LastPlayed map[string]time.Time `json:"lastPlayed"`
		} `json:"podcasts"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Podcasts.Order != "recently-added" || saved.Podcasts.LastPlayed["spotify:show:s1"].IsZero() {
		t.Fatalf("saved history = %s", b)
	}

	again := newStoredHarness(t, api, library.NewStore(path))
	again.send(key("right"))
	again.wantShows("Recently added", "Morning Briefing", "Go Weekly", "History Hour")
	again.send(keys("s", "s")...)
	again.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
}

func TestBothTabsShareTheLibraryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))

	h.send(keys("down", "enter", "s")...)
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	h.send(key("right"))
	h.send(keys("down", "enter")...)
	h.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
	h.send(keys("s", "s")...)
	h.wantShows("Alphabetical", "Go Weekly", "History Hour", "Morning Briefing")

	again := newStoredHarness(t, api, library.NewStore(path))
	again.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	again.send(keys("s", "s", "s")...)
	again.wantOrder("Recents", "Jazz", "Rock", "Samba", "Blues")
	again.send(key("right"))
	again.wantShows("Alphabetical", "Go Weekly", "History Hour", "Morning Briefing")
	again.send(key("s"))
	again.wantShows("Recents", "Go Weekly", "Morning Briefing", "History Hour")
}

func TestPodcastHistorySaveErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "library.json")
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.send(key("right"))

	h.wantView("Fail to save the podcast history")
}

func TestPodcastHistoryReadErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.send(key("right"))

	h.wantView("Fail to read the podcast history", "Morning Briefing")
}

func TestPlaylistsLoadErrorStillLearnsWhatPlaysElsewhere(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:show:s2", "show"
	m := newModelWith(api, 120, 40)
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.pollEvery = time.Minute
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m, holdTimers: true}

	h.send(playlistsLoadFailed{errors.New("service unavailable")})
	h.send(key("right"))

	h.wantShows("Recents", "History Hour", "Morning Briefing", "Go Weekly")
	if len(h.timers) != 1 {
		t.Fatalf("timers = %v, want the next poll", h.timers)
	}
	h.send(playlistsLoadFailed{errors.New("service unavailable")})
	if len(h.timers) != 1 {
		t.Fatalf("timers after a second failure = %v, want a single poll", h.timers)
	}
}

func TestUnfollowedPodcastsLeaveTheHistory(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:gone"}})
	if h.podcasts().history.PodcastLastPlayed("spotify:show:gone").IsZero() {
		t.Fatal("a podcast played elsewhere was not recorded")
	}

	h.send(key("right"))

	if got := h.podcasts().history.PodcastLastPlayed("spotify:show:gone"); !got.IsZero() {
		t.Fatalf("an unfollowed podcast is still in the history: %v", got)
	}
}

func TestPodcastFilterDoesNotChangeThePlaylists(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("/", "j", "enter")...)
	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Jazz"}) {
		t.Fatalf("playlists = %v, want [Jazz]", got)
	}

	h.send(key("right"))
	h.send(keys("/", "w", "enter")...)
	if got := h.showTitles(); !reflect.DeepEqual(got, []string{"Go Weekly"}) {
		t.Fatalf("podcasts = %v, want [Go Weekly]", got)
	}

	h.send(key("left"))
	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Jazz"}) {
		t.Fatalf("playlists after filtering the podcasts = %v, want [Jazz]", got)
	}
	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p1", 0)
}

func TestFilterMatchesGoToTheTabThatAskedForThem(t *testing.T) {
	matches := list.FilterMatchesMsg{}
	filter := func() tea.Msg { return matches }
	other := func() tea.Msg { return pollPlayback{} }

	if got := ownFilterMatches(2, filter)(); !reflect.DeepEqual(got, tabFilterMatches{tab: 2, matches: matches}) {
		t.Fatalf("filter matches = %#v, want them tagged with tab 2", got)
	}
	if got := ownFilterMatches(2, other)(); got != (pollPlayback{}) {
		t.Fatalf("other message = %#v, want it unchanged", got)
	}
	if ownFilterMatches(2, nil) != nil {
		t.Fatal("a nil command was wrapped")
	}

	batch, ok := ownFilterMatches(1, tea.Batch(other, tea.Batch(filter, other)))().(tea.BatchMsg)
	if !ok {
		t.Fatal("a batch is no longer a batch")
	}
	var tagged int
	var run func(tea.BatchMsg)
	run = func(b tea.BatchMsg) {
		for _, c := range b {
			switch msg := c().(type) {
			case tea.BatchMsg:
				run(msg)
			case tabFilterMatches:
				if msg.tab != 1 {
					t.Fatalf("filter matches tagged with tab %d, want 1", msg.tab)
				}
				tagged++
			}
		}
	}
	run(batch)
	if tagged != 1 {
		t.Fatalf("tagged filter matches = %d, want 1", tagged)
	}
}

func TestArrowKeysMoveThePodcastFilterCursor(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("/", "g", "w", "left", "left", "right", "x")...)

	if got := h.player().activeTab; got != podcastsTab {
		t.Fatalf("activeTab = %d, want %d", got, podcastsTab)
	}
	if got := h.podcasts().list.FilterValue(); got != "gxw" {
		t.Fatalf("filter = %q, want %q", got, "gxw")
	}
}

func TestEnterWhileFilteringPodcastsDoesNotPlay(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("/", "w", "enter")...)

	h.wantPlays(0)
	if got := h.podcasts().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	h.send(key("enter"))
	h.wantEpisodePlay(0, "spotify:show:s1", "Episode 1", 0)
}

func TestPlaysWhileTypingSortPodcastsAfterTheFilterCloses(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("/", "w")...)
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:show:s2"}})
	if got := h.showTitles(); !reflect.DeepEqual(got, []string{"Go Weekly"}) {
		t.Fatalf("podcasts while typing = %v, want them untouched", got)
	}
	if got := h.podcasts().list.FilterValue(); got != "w" {
		t.Fatalf("filter = %q, want w", got)
	}

	h.send(key("esc"))
	h.wantShows("Recents", "History Hour", "Morning Briefing", "Go Weekly")
}

func TestPodcastSortKeepsTheAppliedFilter(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())
	h.send(keys("/", "e", "enter")...)
	h.send(key("down"))
	h.wantShows("Recents", "Morning Briefing", "Go Weekly")

	h.send(keys("s", "s")...)

	h.wantShows("Alphabetical", "Go Weekly", "Morning Briefing")
	if got := h.selectedShow(); got != "Go Weekly" {
		t.Fatalf("selected = %q, want Go Weekly", got)
	}
	if got := h.podcasts().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if got := h.podcasts().list.FilterValue(); got != "e" {
		t.Fatalf("filter = %q, want e", got)
	}
}

func TestSortKeyWhileFilteringPodcastsIsTyped(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(keys("/", "s")...)
	if got := h.podcasts().list.Title; got != "Recents" {
		t.Fatalf("typing s in the filter changed the order to %q", got)
	}
	if got := h.podcasts().list.FilterValue(); got != "s" {
		t.Fatalf("filter = %q, want s", got)
	}
}

func TestPodcastEmojiNamesArePrintable(t *testing.T) {
	h := newPodcastHarness(t, newFakeAPI())

	h.send(showsLoaded{shows: []spotify.SavedShow{{ID: "e0", URI: "spotify:show:e0", Name: "Café ☕️ com 👋🏽"}, {ID: "e1", URI: "spotify:show:e1"}}})

	h.wantView("Café ☕ com 👋🏽", "e1")
}

func TestShowItemsAreDefaultItems(t *testing.T) {
	var li list.Item = showItem{title: "Go Weekly"}
	if _, ok := li.(list.DefaultItem); !ok {
		t.Fatal("showItem does not work with the default delegate")
	}
}
