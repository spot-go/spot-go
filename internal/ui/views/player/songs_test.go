package player

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotify"
)

func (h *harness) songs() *playlistSongs {
	return h.playlists().selectedSongs()
}

func (h *harness) songItems() []spotify.PlaylistItem {
	h.t.Helper()

	songs := h.songs()
	if songs == nil || songs.list == nil {
		h.t.Fatalf("songs are not loaded:\n%s", h.view())
	}
	var items []spotify.PlaylistItem
	for _, i := range songs.list.Items() {
		items = append(items, i.(songItem).PlaylistItem)
	}
	return items
}

func (h *harness) songNames() []string {
	h.t.Helper()

	var names []string
	for _, s := range h.songItems() {
		names = append(names, s.Name)
	}
	return names
}

func (h *harness) wantView(texts ...string) {
	h.t.Helper()

	v := h.view()
	for _, text := range texts {
		if !strings.Contains(v, text) {
			h.t.Fatalf("view does not show %q:\n%s", text, v)
		}
	}
}

func (h *harness) dontWantView(texts ...string) {
	h.t.Helper()

	v := h.view()
	for _, text := range texts {
		if strings.Contains(v, text) {
			h.t.Fatalf("view shows %q:\n%s", text, v)
		}
	}
}

func (h *harness) wantPlay(i int, contextURI string, position int) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	got := plays[i]
	want := map[string]any{
		"context_uri": contextURI,
		"offset":      map[string]any{"position": float64(position)},
	}
	if got.deviceID != testDeviceID || !reflect.DeepEqual(got.body, want) {
		h.t.Fatalf("play %d = device %q body %v, want device %q body %v", i, got.deviceID, got.body, testDeviceID, want)
	}
}

func TestSongsOfTheFirstPlaylistLoad(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.wantView("Back in Black - AC/DC", "4:15", "Paranoid - Black Sabbath", "2:48", "Highway Star - Deep Purple", "6:05", "3 songs")
	if got := h.api.requestsFor("p0"); len(got) != 1 {
		t.Fatalf("song requests for p0 = %v, want 1", got)
	}
}

func TestSongsFollowTheSelectedPlaylist(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(key("down"))

	h.wantView("So What - Miles Davis", "9:22")
	h.dontWantView("Back in Black")
}

func TestMovingTheCursorWaitsBeforeLoadingSongs(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.holdTimers = true

	h.send(key("down"))
	if got := h.api.requestsFor("p1"); len(got) != 0 {
		t.Fatalf("songs were requested right after the cursor moved: %v", got)
	}
	h.wantView("Loading songs...")
	if want := []time.Duration{songsLoadDelay}; !reflect.DeepEqual(h.delays, want) || songsLoadDelay <= 0 {
		t.Fatalf("delays = %v, want %v", h.delays, want)
	}

	h.holdTimers = false
	h.send()
	if got := h.api.requestsFor("p1"); len(got) != 1 {
		t.Fatalf("song requests for p1 after the delay = %v, want 1", got)
	}
	h.wantView("So What - Miles Davis")
}

func TestSongsAreLoadedOncePerPlaylist(t *testing.T) {
	h := newHarness(t, 120, 40)

	for _, k := range []string{"down", "up", "down", "up"} {
		h.send(key(k))
	}

	for _, id := range []string{"p0", "p1"} {
		if got := h.api.requestsFor(id); len(got) != 1 {
			t.Fatalf("song requests for %s = %v, want 1", id, got)
		}
	}
}

func TestFastScrollingLoadsOnlyWhereTheCursorStops(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "down", "down", "up", "up")...)

	if got := h.api.requestsFor("p1"); len(got) != 1 {
		t.Fatalf("song requests for p1 = %v, want 1", got)
	}
	for _, id := range []string{"p2", "p3"} {
		if got := h.api.requestsFor(id); len(got) != 0 {
			t.Fatalf("song requests for %s = %v, want none for a playlist scrolled past", id, got)
		}
	}
	h.wantView("So What - Miles Davis")
}

func TestSongsRequestForAnotherPlaylistIsIgnored(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(songsRequested{playlistID: "p2"})

	if got := h.api.requestsFor("p2"); len(got) != 0 {
		t.Fatalf("song requests for p2 = %v, want none while Rock is selected", got)
	}
	if _, ok := h.playlists().songs["p2"]; ok {
		t.Fatal("p2 songs state was created while Rock is selected")
	}
}

func TestAllPagesOfSongsLoad(t *testing.T) {
	api := newFakeAPI()
	api.songs["p2"][60] = `{"is_local":false,"item":null,"track":null}`
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)

	if got, want := api.requestsFor("p2"), []string{"p2@0", "p2@50", "p2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
	songs := h.songItems()
	if len(songs) != 119 {
		t.Fatalf("songs = %d, want 119", len(songs))
	}
	for i, want := range map[int]struct {
		name     string
		position int
	}{
		0:   {"Song 1", 0},
		50:  {"Song 51", 50},
		59:  {"Song 60", 59},
		60:  {"Song 62", 61},
		118: {"Song 120", 119},
	} {
		if got := songs[i]; got.Name != want.name || got.Position != want.position {
			t.Fatalf("song %d = %q at %d, want %q at %d", i, got.Name, got.Position, want.name, want.position)
		}
	}
	h.wantView("119 songs")

	h.songs().list.Select(60)
	h.send(keys("tab", "enter")...)
	h.wantPlay(0, "spotify:playlist:p2", 61)
	h.wantView("Playing: Song 62", "62  Song 62 - Cartola")
}

func TestForbiddenPlaylistExplainsWhy(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "down", "down")...)

	h.wantView("playlists you own or collaborate on", "Press enter to play this playlist")
	if h.songs().state != stateSongsForbidden {
		t.Fatalf("state = %v, want forbidden", h.songs().state)
	}

	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p3", 0)
}

func TestSongsLoadErrorAndRetry(t *testing.T) {
	api := newFakeAPI()
	api.songsStatus["p1"] = 500
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("down"))
	h.wantView("Fail to load songs", "request failed", "Press r to try again")

	api.set(func(f *fakeAPI) { delete(f.songsStatus, "p1") })
	h.send(key("r"))

	h.wantView("So What - Miles Davis")
	if got := api.requestsFor("p1"); len(got) != 2 {
		t.Fatalf("song requests for p1 = %v, want 2", got)
	}
}

func TestNetworkErrorShowsOnlyTheCause(t *testing.T) {
	api := newFakeAPI()
	api.songsErr["p1"] = syscall.ECONNREFUSED
	h := newHarnessWith(t, api, 80, 20)

	h.send(key("down"))

	h.wantView("Fail to load songs", "connection refused", "Press r to try again")
	h.dontWantView("https://", "%2C")
}

func TestRefreshReloadsLoadedSongs(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("tab", "down", "down")...)

	api.set(func(f *fakeAPI) {
		f.songs["p0"] = append([]string{songJSON("Smoke on the Water", "Deep Purple", 340000)}, f.songs["p0"][1:]...)
	})
	h.send(key("r"))

	if got, want := h.songNames(), []string{"Smoke on the Water", "Paranoid", "Highway Star"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs after refresh = %q, want %q", got, want)
	}
	if song, _ := h.playlists().selectedSong(); song.Name != "Highway Star" {
		t.Fatalf("selected song after refresh = %q, want the cursor to stay on Highway Star", song.Name)
	}
	if got := api.requestsFor("p0"); len(got) != 2 {
		t.Fatalf("song requests for p0 = %v, want 2", got)
	}
	h.wantView("r refresh")
	h.dontWantView("refreshing...")
}

func TestRefreshKeepsTheOldSongsWhileLoading(t *testing.T) {
	h := newHarness(t, 120, 40)

	p := h.playlists()
	first := p.refreshSongs("p0")
	h.wantView("Rock (refreshing...)", "Back in Black - AC/DC")

	second := p.refreshSongs("p0")
	h.run(first)
	h.wantView("Rock (refreshing...)")

	h.run(second)
	h.dontWantView("refreshing...")
	if got := len(h.songItems()); got != 3 {
		t.Fatalf("songs = %d, want 3", got)
	}
}

func TestRefreshRestartsAStalledLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["p2@50"] = release
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)
	h.wantView("Samba (loading 50 of 120...)")

	h.send(key("r"))

	if got := len(h.songItems()); got != 120 {
		t.Fatalf("songs = %d, want 120", got)
	}
	h.dontWantView("loading", "refreshing")
	if got, want := api.requestsFor("p2"), []string{"p2@0", "p2@50", "p2@0", "p2@50", "p2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
}

func TestRefreshKeepsTheCursorOnTheSameSong(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("tab", "down", "down")...)

	api.set(func(f *fakeAPI) {
		f.songs["p0"] = append([]string{songJSON("New One", "Someone", 100000)}, f.songs["p0"]...)
	})
	h.send(key("r"))
	if song, _ := h.playlists().selectedSong(); song.Name != "Highway Star" || song.Position != 3 {
		t.Fatalf("selected after an insert above = %q at %d, want Highway Star at 3", song.Name, song.Position)
	}

	api.set(func(f *fakeAPI) { f.songs["p0"] = f.songs["p0"][2:] })
	h.send(key("r"))
	if song, _ := h.playlists().selectedSong(); song.Name != "Highway Star" || song.Position != 1 {
		t.Fatalf("selected after a removal above = %q at %d, want Highway Star at 1", song.Name, song.Position)
	}

	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p0", 1)
}

func TestRefreshIntoAShorterPlaylistClampsTheCursor(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("down", "down")...)
	h.songs().list.Select(110)

	api.set(func(f *fakeAPI) { f.songs["p2"] = f.songs["p2"][:90] })
	h.send(key("r"))
	h.send(keys("tab", "enter")...)

	if song, _ := h.playlists().selectedSong(); song.Name != "Song 90" {
		t.Fatalf("selected = %q, want Song 90", song.Name)
	}
	h.wantPlay(0, "spotify:playlist:p2", 89)
}

func TestFailedRefreshKeepsTheOldSongs(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("down", "down")...)

	api.set(func(f *fakeAPI) {
		f.rateLimits["p2"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"}
	})
	h.send(key("r"))

	if got := len(h.songItems()); got != 120 {
		t.Fatalf("songs after a failed refresh = %d, want the old 120", got)
	}
	h.wantView("Samba (fail to load: API rate limit exceeded)", "120 songs")
}

func TestLongPlaylistNameKeepsTheLoadNote(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p2"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"}
	h := newHarnessWith(t, api, 80, 24)

	h.send(playlistsLoaded{{ID: "p2", URI: "spotify:playlist:p2", Name: "Samba de raiz para ouvir no domingo de manhã com a família toda reunida"}})

	h.wantView("Samba de raiz", "… (fail to load: API rate limit exceeded)")
}

func TestRateLimitedPageIsRetried(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p2"] = rateLimit{offset: "50", times: 2, retryAfter: "0"}
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)

	if got := len(h.songItems()); got != 120 {
		t.Fatalf("songs = %d, want 120", got)
	}
	if got, want := api.requestsFor("p2"), []string{"p2@0", "p2@50", "p2@50", "p2@50", "p2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
}

func TestRetriesStopAfterTheLimit(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p1"] = rateLimit{offset: "0", times: 10, retryAfter: "0"}
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("down"))

	h.wantView("Fail to load songs", "API rate limit exceeded")
	if got := api.requestsFor("p1"); len(got) != 4 {
		t.Fatalf("song requests for p1 = %v, want 1 try and 3 retries", got)
	}
}

func TestServerErrorIsRetried(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p2"] = rateLimit{offset: "50", times: 1, retryAfter: "0", status: 503}
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)

	if got := len(h.songItems()); got != 120 {
		t.Fatalf("songs = %d, want 120", got)
	}
	if got, want := api.requestsFor("p2"), []string{"p2@0", "p2@50", "p2@50", "p2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
}

func TestEmptyPageEndsTheLoad(t *testing.T) {
	api := newFakeAPI()
	api.totals["p1"] = 60
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("down"))

	if got, want := api.requestsFor("p1"), []string{"p1@0", "p1@1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
	h.wantView("So What - Miles Davis")
	h.dontWantView("loading")
}

func TestLongRateLimitIsNotWaitedFor(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p1"] = rateLimit{offset: "0", times: 1, retryAfter: "3600"}
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("down"))

	h.wantView("Fail to load songs", "API rate limit exceeded")
	if got := api.requestsFor("p1"); len(got) != 1 {
		t.Fatalf("song requests for p1 = %v, want 1", got)
	}
}

func TestSongsShowWhileLaterPagesLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["p2@50"] = release
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)

	h.wantView("Samba (loading 50 of 120...)", "50 songs", "Song 1 - Cartola")
	h.send(keys("tab", "down", "enter")...)
	h.wantPlay(0, "spotify:playlist:p2", 1)
}

func TestPartialLoadFailureKeepsTheLoadedSongs(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["p2"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"}
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("down", "down")...)
	h.wantView("Samba (fail to load: API rate limit exceeded)", "50 songs", "Song 1 - Cartola")

	h.send(key("r"))
	if got := len(h.songItems()); got != 120 {
		t.Fatalf("songs after refresh = %d, want 120", got)
	}
	h.dontWantView("fail to load", "loading")
}

func TestStaleSongResultsAreIgnored(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["p2@50"] = release
	h := newHarnessWith(t, api, 120, 40)
	h.send(keys("down", "down")...)
	songs := h.songs()
	if !songs.loading {
		t.Fatal("this test needs a load in progress")
	}

	stale := songs.gen + 1
	h.send(songsPageLoaded{playlistID: "p2", gen: stale, page: spotify.PlaylistItemsPage{Items: []spotify.PlaylistItem{{Name: "Ghost"}}, Last: true}})
	h.send(songsLoadFailed{playlistID: "p2", gen: stale, err: errors.New("ghost error")})

	h.dontWantView("Ghost", "ghost error")
	h.wantView("Samba (loading 50 of 120...)")
	if got := len(h.songItems()); got != 50 {
		t.Fatalf("songs = %d, want 50", got)
	}
}

func TestLongPlaylistNumbersStayAligned(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = manySongsJSON(1001)
	h := newHarnessWith(t, api, 120, 40)

	l := h.songs().list
	if got := len(l.Items()); got != 1001 {
		t.Fatalf("songs = %d, want 1001", got)
	}
	var short, long strings.Builder
	songDelegate{}.Render(&short, *l, 1, l.Items()[1])
	songDelegate{}.Render(&long, *l, 1000, l.Items()[1000])
	if a, b := strings.Index(short.String(), "Song"), strings.Index(long.String(), "Song"); a != b {
		t.Fatalf("song names start at columns %d and %d:\n%q\n%q", a, b, short.String(), long.String())
	}
	if lipgloss.Width(short.String()) != lipgloss.Width(long.String()) {
		t.Fatalf("rows have different widths:\n%q\n%q", short.String(), long.String())
	}
}

func TestEnterPlaysTheSelectedSong(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "down", "enter")...)

	h.wantPlay(0, "spotify:playlist:p0", 1)
	h.wantView("Playing: Paranoid")
	if got := h.selected(); got != "Rock" {
		t.Fatalf("keys on the songs moved the playlist cursor to %q", got)
	}
}

func TestEnterOnAPlaylistPlaysItFromTheStart(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "enter")...)

	h.wantPlay(0, "spotify:playlist:p1", 0)
	h.wantView("Playing: Jazz")
}

func TestEnterWhileFilteringDoesNotPlay(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("/", "j", "a", "z", "enter")...)

	if got := h.api.playRequests(); len(got) != 0 {
		t.Fatalf("enter while filtering started playback: %v", got)
	}

	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p1", 0)
}

func TestPlaybackErrorIsShown(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = 403
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("enter"))

	h.wantView("Fail to play", "Premium required")
}

func TestStalePlaybackResultIsIgnored(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "enter", "down", "enter")...)
	h.send(playbackStarted{seq: 1, name: "Back in Black"})
	h.send(playbackFailed{seq: 1, err: errors.New("late failure")})

	h.wantView("Playing: Paranoid")
	h.dontWantView("Fail to play", "late failure")
}

func TestNoSoundFromSpotifydReplacesPlaying(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(key("enter"))
	h.wantView("Playing: Rock")

	h.send(spotifydPlaybackFailed{errors.New("spotifyd got no audio key from Spotify")})

	h.wantView("No sound: spotifyd got no audio key from Spotify")
	h.dontWantView("Playing: Rock")

	h.send(keys("down", "enter")...)
	h.wantView("Playing: Jazz")
}

func TestPlayerListensForSpotifydErrors(t *testing.T) {
	errs := make(chan error, 1)
	errs <- errors.New("spotifyd got no audio key from Spotify")
	m := newListeningModel(newFakeAPI(), 120, 40, errs)
	h := &harness{t: t, api: newFakeAPI(), model: m}

	h.run(m.Init())

	h.wantView("No sound: spotifyd got no audio key from Spotify", "Back in Black - AC/DC")
}

func TestClosedSpotifydErrorsStopTheListener(t *testing.T) {
	errs := make(chan error)
	close(errs)
	m := newListeningModel(newFakeAPI(), 120, 40, errs)
	h := &harness{t: t, api: newFakeAPI(), model: m}

	h.run(m.Init())

	h.dontWantView("No sound")
	h.wantView("Back in Black - AC/DC")
}

func TestUnplayableSongsAreNotSentToSpotify(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = []string{
		`{"is_local":true,"item":{"type":"track","name":"Demo","uri":"spotify:local:::Demo:100","duration_ms":100000,"artists":[{"name":"Me"}]}}`,
		`{"is_local":false,"item":null,"track":null}`,
		`{"is_local":false,"item":{"type":"track","name":"Gone","uri":"spotify:track:gone","duration_ms":1000,"is_playable":false,"artists":[]}}`,
		`{"is_local":false,"track":{"type":"track","name":"Old Field","uri":"spotify:track:old","duration_ms":61000,"artists":[{"name":"Legacy"}]}}`,
		`{"is_local":false,"item":{"type":"episode","name":"Episode 1","uri":"spotify:episode:e1","duration_ms":3723000,"show":{"name":"A Podcast"}}}`,
	}
	h := newHarnessWith(t, api, 120, 40)

	if got, want := h.songNames(), []string{"Demo", "Gone", "Old Field", "Episode 1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %q, want %q", got, want)
	}
	h.wantView("Old Field - Legacy", "Episode 1 - A Podcast", "1:02:03")

	h.send(keys("tab", "enter")...)
	h.wantView("Local files can't be played from spot-go")
	h.send(keys("down", "enter")...)
	h.wantView("Gone is unavailable")
	if got := api.playRequests(); len(got) != 0 {
		t.Fatalf("unplayable songs were sent to Spotify: %v", got)
	}

	h.send(keys("down", "enter", "down", "enter")...)
	h.wantPlay(0, "spotify:playlist:p0", 3)
	h.wantPlay(1, "spotify:playlist:p0", 4)
}

func TestSongNamesInAnyScriptAreShown(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = []string{
		songJSON("残酷な天使のテーゼ", "高橋洋子", 245000),
		songJSON("Dynamite", "방탄소년단", 199000),
		songJSON("Người Lạ Ơi", "Karik", 260000),
		songJSON("שלום", "عمرو دياب", 200000),
		songJSON("Love ❤️\u00a0you", "Band\u200d👨‍👩‍👧", 100000),
	}
	h := newHarnessWith(t, api, 120, 40)

	h.wantView("残酷な天使のテーゼ - 高橋洋子", "Dynamite - 방탄소년단", "Người Lạ Ơi - Karik", "שלום - عمرو دياب", "Love ❤ you - Band👨👩👧")

	h.send(keys("tab", "enter")...)
	h.wantView("Playing: 残酷な天使のテーゼ")
}

func TestTabMovesTheKeysToTheSongs(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "down", "down")...)
	song, _ := h.playlists().selectedSong()
	if song.Name != "Highway Star" {
		t.Fatalf("selected song = %q, want Highway Star", song.Name)
	}
	h.wantView("tab playlists")

	h.send(keys("tab", "down")...)
	if got := h.selected(); got != "Jazz" {
		t.Fatalf("selected playlist = %q, want Jazz", got)
	}
	h.wantView("tab songs")
}

func TestSongsListFollowsTheWindowSize(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(keys("down", "down")...)

	for _, size := range [][2]int{{61, 9}, {80, 24}, {200, 60}, {120, 40}, {61, 12}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := h.view()
		for i, line := range strings.Split(v, "\n") {
			if got := lipgloss.Width(line); got != size[0] {
				t.Fatalf("%dx%d: line %d is %d columns wide:\n%s", size[0], size[1], i, got, v)
			}
		}
		if got := lipgloss.Height(v); got > size[1] {
			t.Fatalf("%dx%d: view has %d lines:\n%s", size[0], size[1], got, v)
		}

		p := h.playlists()
		l := h.songs().list
		if l.Width() != p.rightWidth() || l.Height() != p.songsHeight() {
			t.Fatalf("%dx%d: songs list is %dx%d, want %dx%d", size[0], size[1], l.Width(), l.Height(), p.rightWidth(), p.songsHeight())
		}
		h.wantView("enter play")
		if p.songsHeight() >= 3 && !strings.Contains(v, "Song 1 - Cartola") {
			t.Fatalf("%dx%d: the first song row is not shown:\n%s", size[0], size[1], v)
		}
	}
}

func TestPlaybackStatusIsShownWhenNoSongRowFits(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(key("enter"))

	h.send(tea.WindowSizeMsg{Width: 120, Height: 9})

	if got := h.playlists().songsHeight(); got != 0 {
		t.Fatalf("songsHeight = %d, want 0 for this test", got)
	}
	h.wantView("Playing: Rock", "enter play")
}

func TestSongsThatFitAreOnOnePage(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = manySongsJSON(13)
	h := newHarnessWith(t, api, 120, 24)

	l := h.songs().list
	if want := l.Height() - 2; len(l.Items()) != want {
		t.Fatalf("this test needs exactly %d songs, got %d", want, len(l.Items()))
	}
	if l.Paginator.TotalPages != 1 {
		t.Fatalf("pages = %d, want 1 for %d songs in a list %d rows tall", l.Paginator.TotalPages, len(l.Items()), l.Height())
	}
	h.wantView("13  Song 13 - Cartola")
}

func TestFit(t *testing.T) {
	for _, tt := range []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 3, "abc"},
		{"abcd", 3, "ab…"},
		{"日本", 4, "日本"},
		{"日本語", 4, "日…"},
		{"abc", 0, ""},
	} {
		if got := fit(tt.in, tt.width); got != tt.want {
			t.Fatalf("fit(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                     "0:00",
		59*time.Second + 999*time.Millisecond: "0:59",
		3*time.Minute + 13*time.Second:        "3:13",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
	} {
		if got := formatDuration(d); got != want {
			t.Fatalf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSongLabelWithoutArtists(t *testing.T) {
	s := songItem{spotify.PlaylistItem{Name: "Solo"}}
	if got := s.label(); got != "Solo" {
		t.Fatalf("label = %q, want Solo", got)
	}
}
