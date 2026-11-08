package player

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/library"
	libspotify "github.com/zmb3/spotify/v2"
)

func newStoredHarness(t *testing.T, api *fakeAPI, store *library.Store) *harness {
	t.Helper()

	m := newStoredModel(api, 120, 40, store)
	h := &harness{t: t, api: api, model: m}
	h.run(m.Init())
	return h
}

func (h *harness) visibleTitles() []string {
	var titles []string
	for _, li := range h.playlists().list.VisibleItems() {
		titles = append(titles, li.(item).title)
	}
	return titles
}

func (h *harness) wantOrder(label string, titles ...string) {
	h.t.Helper()

	if got := h.visibleTitles(); !reflect.DeepEqual(got, titles) {
		h.t.Fatalf("playlists = %v, want %v", got, titles)
	}
	if got := h.playlists().list.Title; got != label {
		h.t.Fatalf("list title = %q, want %q", got, label)
	}
	h.wantView(label)
}

func ago(d time.Duration) time.Time {
	return time.Now().Add(-d).Truncate(time.Second)
}

func TestSortKeyCyclesTheOrders(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")
	h.wantView("s sort")

	h.send(key("down"))
	h.send(key("s"))
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	h.send(key("s"))
	h.wantOrder("Creator", "Blues", "Jazz", "Samba", "Rock")
	h.send(key("s"))
	h.wantOrder("Custom order", "Rock", "Jazz", "Samba", "Blues")
	h.send(key("s"))
	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")

	if got := h.selected(); got != "Jazz" {
		t.Fatalf("selected = %q after sorting, want Jazz", got)
	}
	if got := h.api.requestsFor("p1"); len(got) != 1 {
		t.Fatalf("song requests for p1 = %v, sorting reloaded the songs", got)
	}
}

func TestSortHintSitsUnderTheLibraryList(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.wantView("/ filter • s sort")
	h.dontWantView("refresh • s sort")

	h.send(key("right"))
	h.wantView("/ filter • s sort")
	h.dontWantView("refresh • s sort")
}

func TestSortHintStacksUnderTheFilterHintWhenNarrow(t *testing.T) {
	h := newHarness(t, 80, 40)

	lines := strings.Split(h.view(), "\n")
	for i, line := range lines[:len(lines)-1] {
		if col := strings.Index(line, "/ filter"); col >= 0 {
			if strings.Index(lines[i+1], "s sort") != col {
				t.Fatalf("sort hint is not right below the filter hint:\n%s", h.view())
			}
			return
		}
	}
	t.Fatalf("view does not show the filter hint:\n%s", h.view())
}

func TestSortKeyWorksFromTheSongs(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "s")...)

	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	if got := h.selected(); got != "Rock" {
		t.Fatalf("selected = %q, want Rock", got)
	}
}

func TestSortKeyWhileFilteringIsTyped(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("/", "s")...)

	if got := h.playlists().list.FilterValue(); got != "s" {
		t.Fatalf("filter = %q, want %q", got, "s")
	}
	if got := h.playlists().order(); got != library.OrderRecents {
		t.Fatalf("order = %q while typing in the filter", got)
	}
}

func TestFilterKeepsTheChosenOrder(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("s", "/", "s")...)

	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Blues", "Samba"}) {
		t.Fatalf("filtered playlists = %v, want the alphabetical order", got)
	}
}

func TestSortKeepsTheAppliedFilter(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(keys("s", "s", "s", "/", "a", "enter", "down")...)
	h.wantOrder("Custom order", "Jazz", "Samba")

	h.send(keys("s")...)

	h.wantOrder("Recents", "Jazz", "Samba")
	if got := h.selected(); got != "Samba" {
		t.Fatalf("selected = %q, want Samba", got)
	}
	if got := h.playlists().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if got := h.playlists().list.FilterValue(); got != "a" {
		t.Fatalf("filter = %q, want %q", got, "a")
	}
}

func TestRecentlyPlayedPlaylistsComeFirst(t *testing.T) {
	api := newFakeAPI()
	api.recent = []recentPlay{
		{"playlist", "spotify:playlist:p3", ago(time.Hour)},
		{"album", "spotify:album:x", ago(time.Minute)},
		{"", "", ago(time.Minute)},
		{"playlist", "spotify:playlist:not-in-library", ago(time.Minute)},
		{"playlist", "spotify:playlist:p2", ago(2 * time.Hour)},
		{"playlist", "spotify:playlist:p3", ago(3 * time.Hour)},
	}
	h := newHarnessWith(t, api, 120, 40)

	h.wantOrder("Recents", "Blues", "Samba", "Rock", "Jazz")
	if got := h.selected(); got != "Blues" {
		t.Fatalf("selected = %q, want the first playlist", got)
	}
}

func TestPlayingMovesThePlaylistToTheTop(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "down", "enter")...)

	h.wantPlay(0, "spotify:playlist:p2", 0)
	h.wantOrder("Recents", "Samba", "Rock", "Jazz", "Blues")
	if got := h.selected(); got != "Samba" {
		t.Fatalf("selected = %q, want Samba", got)
	}
}

func TestPlayingKeepsOtherOrders(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("s", "down", "enter")...)

	h.wantPlay(0, "spotify:playlist:p2", 0)
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	if got := h.playlists().history.Playlists["spotify:playlist:p2"].LastPlayed; got.IsZero() {
		t.Fatal("the play was not recorded")
	}
}

func TestPollingRecordsPlaylistsPlayedElsewhere(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p3"
	h := newHarnessWith(t, api, 120, 40)

	h.send(pollPlayback{})
	h.wantOrder("Recents", "Blues", "Rock", "Jazz", "Samba")

	h.send(key("enter"))
	h.wantOrder("Recents", "Rock", "Blues", "Jazz", "Samba")
	api.set(func(f *fakeAPI) { f.playing = "spotify:playlist:p0" })
	h.send(pollPlayback{})
	h.wantOrder("Recents", "Rock", "Blues", "Jazz", "Samba")

	api.set(func(f *fakeAPI) { f.playing = "" })
	h.send(pollPlayback{})
	api.set(func(f *fakeAPI) { f.playing = "spotify:playlist:p3" })
	h.send(pollPlayback{})
	h.wantOrder("Recents", "Blues", "Rock", "Jazz", "Samba")
}

func TestPollAfterAPlayFromSpotGoSeesTheOtherDevice(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p3"
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.send(key("enter"))
	h.wantOrder("Recents", "Rock", "Blues", "Jazz", "Samba")

	h.send(pollPlayback{})

	h.wantOrder("Recents", "Blues", "Rock", "Jazz", "Samba")
}

func TestPausedPlaylistsAreNotRecorded(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p3"
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(pollPlayback{})

	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")
}

func TestFailedPollKeepsTheCurrentPlaylist(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p0"
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	played := h.playlists().history.Playlists["spotify:playlist:p0"].LastPlayed
	if played.IsZero() {
		t.Fatal("the first poll did not record the playlist")
	}

	api.set(func(f *fakeAPI) { f.playerStatus = http.StatusInternalServerError })
	h.send(pollPlayback{})
	api.set(func(f *fakeAPI) { f.playerStatus = 0 })
	h.send(pollPlayback{})

	if got := h.playlists().history.Playlists["spotify:playlist:p0"].LastPlayed; !got.Equal(played) {
		t.Fatalf("last played = %v after a failed poll, want %v: the same playlist was recorded again", got, played)
	}
}

func TestPollingStartsAfterThePlaylistsLoad(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p1"
	m := newModelWith(api, 120, 40)
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.pollEvery = time.Minute
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m, holdTimers: true}

	h.run(m.Init())
	h.wantOrder("Recents", "Jazz", "Rock", "Samba", "Blues")
	if !reflect.DeepEqual(h.delays, []time.Duration{time.Minute}) || len(h.timers) != 1 {
		t.Fatalf("delays = %v timers = %v, want one poll after a minute", h.delays, h.timers)
	}

	h.send(playlistsLoadFailed{errors.New("service unavailable")})
	h.send(key("r"))
	if got := len(h.timers); got != 1 {
		t.Fatalf("timers = %d after reloading, want a single poll", got)
	}
	api.set(func(f *fakeAPI) {
		if f.playerRequests != 1 {
			t.Fatalf("player requests = %d, want 1", f.playerRequests)
		}
	})
}

func TestNewPlaylistsCountAsRecentlyAdded(t *testing.T) {
	api := newFakeAPI()
	api.recent = []recentPlay{{"playlist", "spotify:playlist:p3", ago(time.Hour)}}
	h := newHarnessWith(t, api, 120, 40)

	loaded := []libspotify.SimplePlaylist{
		{ID: "p0", URI: "spotify:playlist:p0", Name: "Rock"},
		{ID: "p3", URI: "spotify:playlist:p3", Name: "Blues"},
		{ID: "p9", URI: "spotify:playlist:p9", Name: "Forró"},
	}
	h.send(playlistsLoaded{playlists: loaded})

	h.wantOrder("Recents", "Forró", "Blues", "Rock")
}

func TestOrderAndHistoryAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))

	h.send(keys("down", "down", "enter", "s")...)
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"order":"alphabetical"`) || !strings.Contains(string(b), "spotify:playlist:p2") {
		t.Fatalf("saved history = %s", b)
	}

	again := newStoredHarness(t, api, library.NewStore(path))
	again.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	again.send(key("s"))
	again.send(key("s"))
	again.send(key("s"))
	again.wantOrder("Recents", "Samba", "Rock", "Jazz", "Blues")
}

func TestUnknownSavedOrderFallsBackToRecents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte(`{"order":"shuffled"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")
}

func TestBrokenHistoryIsShownAndReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.wantView("Fail to read the playlist history")
	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")
	if _, err := library.NewStore(path).Load(); err != nil {
		t.Fatalf("history was not replaced: %v", err)
	}
}

func TestHistorySaveErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "library.json")

	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.wantView("Fail to save the playlist history")
}

func TestSortItems(t *testing.T) {
	items := func(names ...string) []item {
		var its []item
		for i, n := range names {
			title, owner, _ := strings.Cut(n, "/")
			its = append(its, item{title: title, owner: owner, index: i, uri: libspotify.URI("spotify:playlist:" + title)})
		}
		return its
	}
	titles := func(order library.Order, its []item) string {
		in := make([]list.Item, len(its))
		for i, it := range its {
			in[i] = it
		}
		var out []string
		for _, li := range sortItems(in, order, library.History{}) {
			out = append(out, li.(item).title)
		}
		return strings.Join(out, ",")
	}

	alphabetical := items("Zebra", "água", "Abacaxi", "ária", "abacaxi")
	if got, want := titles(library.OrderAlphabetical, alphabetical), "Abacaxi,abacaxi,água,ária,Zebra"; got != want {
		t.Fatalf("alphabetical = %s, want %s", got, want)
	}

	creators := items("b/Érica", "a/zé", "c/Érica", "a/Érica", "d/")
	if got, want := titles(library.OrderCreator, creators), "d,a,b,c,a"; got != want {
		t.Fatalf("creator = %s, want %s", got, want)
	}
	if got, want := titles(library.OrderCustom, creators), "b,a,c,a,d"; got != want {
		t.Fatalf("custom = %s, want %s", got, want)
	}
}

func TestPlaysWhileTypingSortAfterTheFilterCloses(t *testing.T) {
	api := newFakeAPI()
	api.playing = "spotify:playlist:p2"
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("/", "a")...)
	h.send(pollPlayback{})
	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Jazz", "Samba"}) {
		t.Fatalf("playlists while typing = %v, want them untouched", got)
	}
	if got := h.playlists().list.FilterValue(); got != "a" {
		t.Fatalf("filter = %q, want %q", got, "a")
	}

	h.send(key("esc"))
	h.wantOrder("Recents", "Samba", "Rock", "Jazz", "Blues")
}

func TestOnlyChangingTheOrderIsSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	newStoredHarness(t, api, library.NewStore(path)).send(key("s"))

	again := newStoredHarness(t, api, library.NewStore(path))

	again.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
}

func TestUnreadableHistoryIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	saved := []byte(`{"order":"creator","synced":true}`)
	if err := os.WriteFile(path, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("the file is still readable, probably running as root")
	}

	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))
	h.wantView("Fail to read the playlist history")
	h.send(keys("s", "enter")...)

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != string(saved) {
		t.Fatalf("history = %s, %v, want it untouched", b, err)
	}
}

func TestCreatorFallsBackToTheOwnerID(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(playlistsLoaded{playlists: []libspotify.SimplePlaylist{
		{ID: "p0", URI: "spotify:playlist:p0", Name: "Rock", Owner: libspotify.User{DisplayName: "Zeca"}},
		{ID: "p1", URI: "spotify:playlist:p1", Name: "Jazz", Owner: libspotify.User{ID: "bia"}},
		{ID: "p2", URI: "spotify:playlist:p2", Name: "Samba", Owner: libspotify.User{DisplayName: "Ana"}},
	}})
	h.send(keys("s", "s")...)

	h.wantOrder("Creator", "Samba", "Jazz", "Rock")
}

func TestOrderLabelStaysOnTheFirstRow(t *testing.T) {
	h := newHarness(t, 1, 24)
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, label := range []string{"Alphabetical", "Creator", "Custom order", "Recents"} {
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

func TestSortAlbums(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	items := []list.Item{
		albumItem{title: "b", artist: "Zé", uri: "spotify:album:b", addedAt: day(5), index: 0},
		albumItem{title: "a", artist: "ana", uri: "spotify:album:a", addedAt: day(5), index: 1},
		albumItem{title: "c", artist: "Ana", uri: "spotify:album:c", addedAt: day(9), index: 2},
		albumItem{title: "d", artist: "Bia", uri: "spotify:album:d", addedAt: day(1), index: 3},
	}
	var history library.History
	history.AlbumPlayed("spotify:album:d", day(3))
	history.AlbumPlayed("spotify:album:a", day(7))

	for _, tt := range []struct {
		order library.Order
		want  []string
	}{
		{library.OrderRecents, []string{"c", "a", "b", "d"}},
		{library.OrderRecentlyAdded, []string{"c", "b", "a", "d"}},
		{library.OrderAlphabetical, []string{"a", "b", "c", "d"}},
		{library.OrderCreator, []string{"a", "c", "d", "b"}},
		{library.OrderCustom, []string{"c", "a", "b", "d"}},
	} {
		var got []string
		for _, li := range sortAlbums(items, tt.order, history) {
			got = append(got, li.(albumItem).title)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("sortAlbums(%s) = %v, want %v", tt.order, got, tt.want)
		}
	}
}

func TestSortArtists(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	uris := []libspotify.URI{"spotify:artist:b", "spotify:artist:a", "spotify:artist:c", "spotify:artist:d"}
	items := []list.Item{
		artistItem{title: "b", uri: uris[0], index: 0},
		artistItem{title: "á", uri: uris[1], index: 1},
		artistItem{title: "c", uri: uris[2], index: 2},
		artistItem{title: "d", uri: uris[3], index: 3},
	}
	var history library.History
	history.SyncArtists(uris[:2], day(1))
	history.SyncArtists(uris, day(4))
	history.ArtistPlayed("spotify:artist:a", day(9))
	history.ArtistPlayed("spotify:artist:b", day(2))

	for _, tt := range []struct {
		order library.Order
		want  []string
	}{
		{library.OrderRecents, []string{"á", "c", "d", "b"}},
		{library.OrderRecentlyAdded, []string{"c", "d", "b", "á"}},
		{library.OrderAlphabetical, []string{"á", "b", "c", "d"}},
		{library.OrderCreator, []string{"á", "c", "d", "b"}},
	} {
		var got []string
		for _, li := range sortArtists(items, tt.order, history) {
			got = append(got, li.(artistItem).title)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("sortArtists(%s) = %v, want %v", tt.order, got, tt.want)
		}
	}
}
