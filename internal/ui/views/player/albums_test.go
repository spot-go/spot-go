package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

func albumTrackURI(name string) string {
	return fmt.Sprintf("spotify:track:%x", name)
}

func (f *fakeAPI) savedAlbumLoads() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.albumRequests
}

func (h *harness) albumTab() albumModel {
	return h.player().tabs[albumsTab].content.(albumModel)
}

func (h *harness) setAlbumTab(a albumModel) {
	m := h.player()
	m.tabs[albumsTab].content = a
	h.model = m
}

func (h *harness) openAlbumsTab() {
	h.t.Helper()

	model, cmd := h.player().switchTab(albumsTab)
	h.model = model
	h.run(cmd)
}

func newAlbumsHarness(t *testing.T, api *fakeAPI) *harness {
	t.Helper()

	h := newHarnessWith(t, api, 120, 40)
	h.openAlbumsTab()
	return h
}

func (h *harness) selectedSavedAlbum() string {
	a, _ := h.albumTab().selectedAlbum()
	return a.title
}

func (h *harness) savedAlbumTitles() []string {
	var titles []string
	for _, li := range h.albumTab().list.VisibleItems() {
		titles = append(titles, li.(albumItem).title)
	}
	return titles
}

func (h *harness) wantSavedAlbums(label string, titles ...string) {
	h.t.Helper()

	if got := h.savedAlbumTitles(); !reflect.DeepEqual(got, titles) {
		h.t.Fatalf("albums = %v, want %v", got, titles)
	}
	if got := h.albumTab().list.Title; got != label {
		h.t.Fatalf("list title = %q, want %q", got, label)
	}
	h.wantView(label)
}

func (h *harness) albumSongs() *playlistSongs {
	return h.albumTab().selectedSongs()
}

func (h *harness) albumSongItems() []spotify.PlaylistItem {
	h.t.Helper()

	songs := h.albumSongs()
	if songs == nil || songs.list == nil {
		h.t.Fatalf("album songs are not loaded:\n%s", h.view())
	}
	var items []spotify.PlaylistItem
	for _, li := range songs.list.Items() {
		items = append(items, li.(songItem).PlaylistItem)
	}
	return items
}

func (h *harness) albumSongNames() []string {
	h.t.Helper()

	var names []string
	for _, s := range h.albumSongItems() {
		names = append(names, s.Name)
	}
	return names
}

func (h *harness) selectedAlbumSong() spotify.PlaylistItem {
	s, _ := h.albumTab().selectedSong()
	return s
}

func (h *harness) settleAlbums() {
	a := h.albumTab()
	a.playback.since = time.Time{}
	h.setAlbumTab(a)
	m := h.player()
	m.nowPlaying.commandAt = time.Time{}
	h.model = m
}

func TestSavedAlbumsLoadWhenTheTabOpens(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	if got := api.savedAlbumLoads(); got != 0 {
		t.Fatalf("albums were requested %d times before the tab opened", got)
	}

	h.send(keys("right", "right", "right")...)
	if got := h.player().activeTab; got != albumsTab {
		t.Fatalf("activeTab = %d, want the Albums tab %d", got, albumsTab)
	}
	h.wantView("Abbey Road", "Kind of Blue", "Clube da Esquina", "3 albums")
	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")

	h.send(keys("left", "right")...)
	if got := api.savedAlbumLoads(); got != 1 {
		t.Fatalf("albums were requested %d times, want 1", got)
	}
	if h.albumLoads != 1 {
		t.Fatalf("albums loaded %d times, want 1", h.albumLoads)
	}
	if got, want := api.requestsFor("a0"), []string{"a0@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
}

func TestAllPagesOfSavedAlbumsLoad(t *testing.T) {
	api := newFakeAPI()
	api.albums = nil
	for i := range 70 {
		api.albums = append(api.albums, savedAlbumJSON(fmt.Sprintf("x%d", i), fmt.Sprintf("Album %d", i), "Someone", "2026-01-01T00:00:00Z"))
	}
	h := newAlbumsHarness(t, api)

	if got := len(h.albumTab().list.Items()); got != 70 {
		t.Fatalf("albums = %d, want 70", got)
	}
	if got := api.savedAlbumLoads(); got != 2 {
		t.Fatalf("album pages requested = %d, want 2", got)
	}
}

func TestSongsOfTheFirstAlbumLoad(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.wantView("Abbey Road - The Beatles", "1  Come Together - The Beatles", "4:19", "2  Something - The Beatles", "3:02", "3  Her Majesty - The Beatles", "0:23", "3 songs")
	if got, want := h.albumSongNames(), []string{"Come Together", "Something", "Her Majesty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want %v", got, want)
	}
}

func TestUnplayableAlbumSongsAreFaint(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	l := h.albumSongs().list
	faint := lipgloss.NewStyle().Faint(true).Render("x")
	marker := faint[:strings.Index(faint, "x")]
	for i, li := range l.Items() {
		var b strings.Builder
		songDelegate{}.Render(&b, *l, i, li)
		song := li.(songItem)
		if got := strings.Contains(b.String(), marker); got != !song.Playable {
			t.Fatalf("%s is faint = %v, want %v: %q", song.Name, got, !song.Playable, b.String())
		}
	}
}

func TestNoSavedAlbums(t *testing.T) {
	api := newFakeAPI()
	api.albums = nil
	h := newAlbumsHarness(t, api)

	h.wantView("No albums", "You haven't saved any albums.", "then press r")
	api.set(func(f *fakeAPI) { f.albums = testSavedAlbumsJSON() })
	h.send(key("r"))

	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	if h.albumLoads != 2 {
		t.Fatalf("albums loaded %d times, want 2", h.albumLoads)
	}
}

func TestSavedAlbumsLoadErrorAndRetry(t *testing.T) {
	api := newFakeAPI()
	api.albumsStatus = http.StatusInternalServerError
	h := newAlbumsHarness(t, api)

	h.wantView("Fail to load albums", "request failed", "Press r to try again")
	h.send(keys("down", "enter", "s", "tab", "enter")...)
	h.wantPlays(0)

	api.set(func(f *fakeAPI) { f.albumsStatus = 0 })
	h.send(key("r"))
	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	if got := api.savedAlbumLoads(); got != 2 {
		t.Fatalf("albums were requested %d times, want 2", got)
	}
}

func TestSavedAlbumsLoadedWhileOnAnotherTab(t *testing.T) {
	h := newHarness(t, 120, 40)
	model, cmd := h.player().switchTab(albumsTab)
	h.model = model

	h.send(key("left"))
	h.run(cmd)
	h.openAlbumsTab()

	if h.albumTab().state != stateSavedAlbumsLoaded {
		t.Fatalf("state = %v, want loaded", h.albumTab().state)
	}
	h.wantView("Abbey Road", "Come Together")
}

func TestResizeKeepsTheSelectedAlbum(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(key("down"))

	for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	}
	h.send(key("left"))
	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	h.send(key("right"))

	if got := h.selectedSavedAlbum(); got != "Kind of Blue" {
		t.Fatalf("selected = %q, want Kind of Blue", got)
	}
	if got, want := tabSize(h.albumTab()), h.player().contentDimension(); got != want {
		t.Fatalf("albums tab size = %+v, want %+v", got, want)
	}
	if h.albumLoads != 1 {
		t.Fatalf("albums loaded %d times, want 1", h.albumLoads)
	}
}

func TestMovingTheAlbumCursorWaitsBeforeLoadingSongs(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.holdTimers = true
	h.delays = nil

	h.send(key("down"))
	if got := api.requestsFor("a1"); len(got) != 0 {
		t.Fatalf("songs were requested right after the cursor moved: %v", got)
	}
	h.wantView("Kind of Blue - Miles Davis", "Loading songs...")
	if want := []time.Duration{songsLoadDelay}; !reflect.DeepEqual(h.delays, want) {
		t.Fatalf("delays = %v, want %v", h.delays, want)
	}

	h.holdTimers = false
	h.send()
	if got, want := api.requestsFor("a1"), []string{"a1@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
	h.wantView("So What - Miles Davis", "9:22")
	h.dontWantView("Come Together")
}

func TestFastScrollingLoadsOnlyTheSelectedAlbum(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "down")...)

	if got := api.requestsFor("a1"); len(got) != 0 {
		t.Fatalf("song requests for a1 = %v, want none for an album scrolled past", got)
	}
	if got, want := api.requestsFor("a2"), []string{"a2@0", "a2@50", "a2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests for a2 = %v, want %v", got, want)
	}
}

func TestAlbumSongsAreLoadedOncePerAlbum(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)

	for _, k := range []string{"down", "up", "down", "up"} {
		h.send(key(k))
	}

	for _, id := range []string{"a0", "a1"} {
		if got := api.requestsFor(id); len(got) != 1 {
			t.Fatalf("song requests for %s = %v, want 1", id, got)
		}
	}
}

func TestAlbumSongsRequestForAnotherAlbumIsIgnored(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)

	h.send(albumSongsRequested{albumID: "a2"})

	if got := api.requestsFor("a2"); len(got) != 0 {
		t.Fatalf("song requests for a2 = %v, want none while Abbey Road is selected", got)
	}
	if _, ok := h.albumTab().songs["a2"]; ok {
		t.Fatal("a2 songs state was created while Abbey Road is selected")
	}
}

func TestAllPagesOfAlbumSongsLoad(t *testing.T) {
	api := newFakeAPI()
	api.albumTracks["a2"][60] = "null"
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "down")...)

	if got, want := api.requestsFor("a2"), []string{"a2@0", "a2@50", "a2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
	songs := h.albumSongItems()
	if len(songs) != 119 {
		t.Fatalf("songs = %d, want 119", len(songs))
	}
	for i, want := range map[int]struct {
		name     string
		position int
	}{
		0:   {"Track 1", 0},
		50:  {"Track 51", 50},
		59:  {"Track 60", 59},
		60:  {"Track 62", 61},
		118: {"Track 120", 119},
	} {
		if got := songs[i]; got.Name != want.name || got.Position != want.position {
			t.Fatalf("song %d = %q at %d, want %q at %d", i, got.Name, got.Position, want.name, want.position)
		}
	}
	h.wantView("Clube da Esquina - Milton Nascimento", "119 songs")

	h.albumSongs().list.Select(60)
	h.send(keys("tab", "enter")...)
	h.wantPlay(0, "spotify:album:a2", 61)
	h.wantView("⏸ Track 62", "62  Track 62 - Milton Nascimento")
}

func TestAlbumSongsShowWhileLaterPagesLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["a2@50"] = release
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "down")...)

	h.wantView("Clube da Esquina - Milton Nascimento (loading 50 of 120...)", "50 songs", "Track 1 - Milton Nascimento")
	h.send(keys("tab", "down", "enter")...)
	h.wantPlay(0, "spotify:album:a2", 1)
}

func TestEmptyAlbumShowsANotice(t *testing.T) {
	api := newFakeAPI()
	api.albumTracks["a1"] = nil
	h := newAlbumsHarness(t, api)

	h.send(key("down"))
	h.wantView("Kind of Blue - Miles Davis", noAlbumSongs)

	h.send(keys("tab", "enter")...)
	h.wantPlay(0, "spotify:album:a1", 0)
	h.wantView("⏸ Kind of Blue")
}

func TestAlbumSongsLoadErrorAndRetry(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		api := newFakeAPI()
		api.songsStatus["a0"] = status
		h := newAlbumsHarness(t, api)

		h.wantView("Fail to load songs", "request failed", "Press r to try again")
		h.dontWantView("playlists you own")

		api.set(func(f *fakeAPI) { delete(f.songsStatus, "a0") })
		h.send(key("r"))
		h.wantView("Come Together - The Beatles")
		if got := api.requestsFor("a0"); len(got) != 2 {
			t.Fatalf("song requests for a0 = %v, want 2", got)
		}
	}
}

func TestAlbumSongsNetworkErrorShowsOnlyTheCause(t *testing.T) {
	api := newFakeAPI()
	api.songsErr["a1"] = syscall.ECONNREFUSED
	h := newAlbumsHarness(t, api)

	h.send(key("down"))

	h.wantView("Fail to load songs", "connection refused", "Press r to try again")
	h.dontWantView("https://", "%2C")
}

func TestPartialAlbumSongsFailureKeepsTheLoadedSongs(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["a2"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"}
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "down")...)
	h.wantView("Clube da Esquina - Milton Nascimento (fail to load: API rate limit exceeded)", "50 songs", "Track 1 - Milton Nascimento")

	h.send(key("r"))
	if got := len(h.albumSongItems()); got != 120 {
		t.Fatalf("songs after refresh = %d, want 120", got)
	}
	h.dontWantView("fail to load", "loading")
}

func TestRefreshReloadsTheAlbumSongs(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.send(keys("tab", "down", "down")...)

	api.set(func(f *fakeAPI) {
		f.albumTracks["a0"] = append([]string{albumTrackJSON("Octopus's Garden", "The Beatles", 171000, true)}, f.albumTracks["a0"][1:]...)
	})
	h.send(key("r"))

	if got, want := h.albumSongNames(), []string{"Octopus's Garden", "Something", "Her Majesty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs after refresh = %q, want %q", got, want)
	}
	if got := h.selectedAlbumSong(); got.Name != "Her Majesty" {
		t.Fatalf("selected song after refresh = %q, want the cursor to stay on Her Majesty", got.Name)
	}
	if got := api.requestsFor("a0"); len(got) != 2 {
		t.Fatalf("song requests for a0 = %v, want 2", got)
	}
	if got := h.selectedSavedAlbum(); got != "Abbey Road" {
		t.Fatalf("selected album after refresh = %q, want Abbey Road", got)
	}
	h.wantView("r refresh")
	h.dontWantView("refreshing...")
}

func TestRefreshKeepsTheAlbumSongsWhileLoading(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	a := h.albumTab()
	first := a.refreshSongs("a0")
	h.wantView("Abbey Road - The Beatles (refreshing...)", "Come Together - The Beatles")

	second := a.refreshSongs("a0")
	h.run(first)
	h.wantView("Abbey Road - The Beatles (refreshing...)")

	h.run(second)
	h.dontWantView("refreshing...")
	if got := len(h.albumSongItems()); got != 3 {
		t.Fatalf("songs = %d, want 3", got)
	}
}

func TestFailedRefreshKeepsTheAlbumSongs(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)

	api.set(func(f *fakeAPI) { f.songsStatus["a0"] = http.StatusInternalServerError })
	h.send(key("r"))

	h.wantView("Abbey Road - The Beatles (fail to load: request failed)", "Come Together - The Beatles")
	if got := len(h.albumSongItems()); got != 3 {
		t.Fatalf("songs = %d, want 3", got)
	}
}

func TestRefreshRestartsAStalledAlbumSongsLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["a2@50"] = release
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "down")...)
	h.wantView("Clube da Esquina - Milton Nascimento (loading 50 of 120...)")

	h.send(key("r"))

	if got := len(h.albumSongItems()); got != 120 {
		t.Fatalf("songs = %d, want 120", got)
	}
	h.dontWantView("loading", "refreshing")
	if got, want := api.requestsFor("a2"), []string{"a2@0", "a2@50", "a2@0", "a2@50", "a2@100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("song requests = %v, want %v", got, want)
	}
}

func TestStaleAlbumSongResultsAreIgnored(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["a2@50"] = release
	h := newAlbumsHarness(t, api)
	h.send(keys("down", "down")...)
	songs := h.albumSongs()
	if !songs.loading {
		t.Fatal("this test needs a load in progress")
	}

	ghost := spotify.PlaylistItemsPage{Items: []spotify.PlaylistItem{{Name: "Ghost"}}, Last: true}
	for _, gen := range []int{songs.gen - 1, songs.gen + 1} {
		h.send(albumSongsPageLoaded{albumID: "a2", gen: gen, page: ghost})
		h.send(albumSongsLoadFailed{albumID: "a2", gen: gen, err: errors.New("ghost error")})
	}
	h.send(albumSongsPageLoaded{albumID: "gone", gen: 0, page: ghost})

	h.dontWantView("Ghost", "ghost error")
	h.wantView("Clube da Esquina - Milton Nascimento (loading 50 of 120...)")
	if got := len(h.albumSongItems()); got != 50 {
		t.Fatalf("songs = %d, want 50", got)
	}
}

func TestRefreshingTwiceKeepsTheSelectedAlbumSong(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.send(keys("down", "down", "tab")...)
	for range 80 {
		h.send(key("down"))
	}
	if got := h.selectedAlbumSong(); got.Name != "Track 81" {
		t.Fatalf("selected = %q, want Track 81", got.Name)
	}

	api.set(func(f *fakeAPI) { f.rateLimits["a2"] = rateLimit{offset: "50", times: 1, retryAfter: "3600"} })
	h.send(key("r"))
	h.wantView("Clube da Esquina - Milton Nascimento (fail to load: API rate limit exceeded)")
	h.send(key("r"))

	if got := len(h.albumSongItems()); got != 120 {
		t.Fatalf("songs = %d, want 120", got)
	}
	if got := h.selectedAlbumSong(); got.Name != "Track 81" {
		t.Fatalf("selected after refreshing twice = %q, want Track 81", got.Name)
	}
}

func TestRefreshIntoAShorterAlbumClampsTheCursor(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.send(keys("down", "down")...)
	h.albumSongs().list.Select(110)

	api.set(func(f *fakeAPI) { f.albumTracks["a2"] = f.albumTracks["a2"][:90] })
	h.send(key("r"))
	h.send(keys("tab", "enter")...)

	if got := h.selectedAlbumSong(); got.Name != "Track 90" {
		t.Fatalf("selected = %q, want Track 90", got.Name)
	}
	h.wantPlay(0, "spotify:album:a2", 89)
}

func TestAlbumsViewFitsTheWindow(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
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
	h.wantView("Kind of Blue", "So What")
}

func TestAlbumsFrameReachesTheRightEdge(t *testing.T) {
	api := newFakeAPI()
	api.albums = append(api.albums, savedAlbumJSON("a3", "👩🏾‍❤️‍👩🏼 casal 🇧🇷 com um nome bem comprido 👋🏿", "Banda 🧑🏿‍🎤", "2024-01-01T00:00:00Z"))
	api.albumTracks["a0"] = append(api.albumTracks["a0"], albumTrackJSON("Tchau 👋🏽 e um título bem comprido", "Banda 🧑🏿‍🎤", 100000, true))
	h := newAlbumsHarness(t, api)
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, focus := range [][]string{nil, {"tab"}, {"tab", "end"}} {
		h.send(keys(focus...)...)
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
	if got := h.selectedSavedAlbum(); !strings.Contains(got, "casal") {
		t.Fatalf("selected = %q, want the album with the long name", got)
	}
}

func TestAlbumOrderLabelStaysOnTheFirstRow(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, label := range []string{"Recently added", "Alphabetical", "Creator", "Recents"} {
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

func TestAlbumSongsListFollowsTheWindowSize(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
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

		a := h.albumTab()
		l := h.albumSongs().list
		if l.Width() != a.rightWidth() || l.Height() != a.listHeight() {
			t.Fatalf("%dx%d: songs list is %dx%d, want %dx%d", size[0], size[1], l.Width(), l.Height(), a.rightWidth(), a.listHeight())
		}
		h.wantView("enter play")
		if a.listHeight() >= 3 && !strings.Contains(v, "Track 1 - Milton Nascimento") {
			t.Fatalf("%dx%d: the first song row is not shown:\n%s", size[0], size[1], v)
		}
	}
}

func TestAlbumsViewDoesNotStartGoroutines(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	m := h.player()

	before := runtime.NumGoroutine()
	for range 200 {
		_ = m.View()
	}
	if after := runtime.NumGoroutine(); after-before > 20 {
		t.Fatalf("View started goroutines: %d before, %d after", before, after)
	}
}

func TestEnterOnASavedAlbumPlaysItFromTheStart(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("down", "enter")...)

	h.wantPlay(0, "spotify:album:a1", 0)
	h.wantPlays(1)
	h.wantView("⏸ Kind of Blue")
}

func TestPlayingAnAlbumSaysWhatItStarts(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	a, cmd := h.albumTab().play()
	if cmd == nil || a.playback.text != "Starting Abbey Road..." || !a.playback.starting {
		t.Fatalf("play on the albums = %+v", a.playback)
	}

	h.send(keys("tab", "down")...)
	a, cmd = h.albumTab().play()
	if cmd == nil || a.playback.text != "Starting Something..." || !a.playback.starting {
		t.Fatalf("play on the songs = %+v", a.playback)
	}
	if got, want := cmd(), (albumPlaybackStarted{seq: a.playback.seq, name: "Something", artist: "The Beatles", duration: 182 * time.Second, uri: "spotify:album:a0", item: libspotify.URI(albumTrackURI("Something"))}); got != want {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}

func TestEnterPlaysTheAlbumFromTheSelectedSong(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("tab", "down", "enter")...)

	h.wantPlay(0, "spotify:album:a0", 1)
	h.wantView("⏸ Something")
	if got := h.selectedSavedAlbum(); got != "Abbey Road" {
		t.Fatalf("keys on the songs moved the album cursor to %q", got)
	}
}

func TestEnterBeforeTheAlbumSongsLoadPlaysTheAlbum(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.holdTimers = true

	h.send(keys("down", "tab", "enter")...)

	h.wantPlay(0, "spotify:album:a1", 0)
	h.wantView("⏸ Kind of Blue")
	if got := api.requestsFor("a1"); len(got) != 0 {
		t.Fatalf("song requests for a1 = %v, want none before the delay", got)
	}
}

func TestUnplayableAlbumSongIsNotSentToSpotify(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("tab", "down", "down", "enter")...)

	h.wantView("Her Majesty is unavailable")
	h.wantPlays(0)

	h.send(keys("tab", "enter")...)
	h.wantPlay(0, "spotify:album:a0", 0)
	h.wantView("⏸ Abbey Road")
}

func TestAlbumPlaybackErrorIsShown(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newAlbumsHarness(t, api)

	h.send(key("enter"))

	h.wantView("Fail to play", "Premium required")
}

func TestAlbumPlaybackErrorStaysWhileOtherSongsPlay(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newAlbumsHarness(t, api)
	h.send(key("enter"))

	api.playsElsewhere("spotify:album:a1", "album", albumTrackURI("So What"), "So What")
	h.send(pollPlayback{})

	h.wantView("Fail to play", "Premium required")
	h.dontWantView("⏸ So What")
}

func TestStaleAlbumPlaybackResultIsIgnored(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("tab", "enter", "down", "enter")...)
	h.send(albumPlaybackStarted{seq: 1, name: "Come Together", uri: "spotify:album:a0"})
	h.send(albumPlaybackFailed{seq: 1, err: errors.New("late failure")})

	h.wantView("⏸ Something")
	h.dontWantView("Fail to play", "late failure", "⏸ Come Together")
}

func TestAlbumsTabKeepsItsOwnPlaybackStatus(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(key("enter"))
	seq := h.albumTab().playback.seq
	h.send(playbackStarted{seq: seq, name: "Rock", uri: "spotify:playlist:p0"})
	h.send(episodePlaybackStarted{seq: seq, name: "Trailer", show: "spotify:show:s0"})
	h.send(playbackFailed{seq: seq, err: errors.New("playlist failure")})
	h.send(episodePlaybackFailed{seq: seq, err: errors.New("episode failure")})
	h.wantView("⏸ Abbey Road")
	h.dontWantView("Rock", "Trailer", "failure")

	h.send(keys("left", "left", "left")...)
	if got := h.player().activeTab; got != playlistsTab {
		t.Fatalf("activeTab = %d, want %d", got, playlistsTab)
	}
	h.dontWantView("Abbey Road")
}

func TestAlbumSortKeyCyclesTheOrders(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:a2"}})
	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")
	h.wantView("/ filter • s sort")

	h.send(key("s"))
	h.wantSavedAlbums("Recently added", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	h.send(key("s"))
	h.wantSavedAlbums("Alphabetical", "Abbey Road", "Clube da Esquina", "Kind of Blue")
	h.send(key("s"))
	h.wantSavedAlbums("Creator", "Kind of Blue", "Clube da Esquina", "Abbey Road")
	h.send(key("s"))
	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")
}

func TestAlbumOrdersUseWhenTheAlbumsWereSaved(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(albumsLoaded{albums: []spotify.SavedAlbum{
		{Album: spotify.Album{ID: "z", URI: "spotify:album:z", Name: "Zebra Songs", Artists: []string{"Bia"}}, AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Album: spotify.Album{ID: "a", URI: "spotify:album:a", Name: "Apple Tunes", Artists: []string{"Zeca"}}, AddedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Album: spotify.Album{ID: "m", URI: "spotify:album:m", Name: "Mango Beats", Artists: []string{"ana", "Zeca"}}, AddedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)},
	}})
	h.wantSavedAlbums("Recents", "Apple Tunes", "Zebra Songs", "Mango Beats")

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:m"}})
	h.wantSavedAlbums("Recents", "Mango Beats", "Apple Tunes", "Zebra Songs")
	h.send(key("s"))
	h.wantSavedAlbums("Recently added", "Apple Tunes", "Zebra Songs", "Mango Beats")
	h.send(key("s"))
	h.wantSavedAlbums("Alphabetical", "Apple Tunes", "Mango Beats", "Zebra Songs")
	h.send(key("s"))
	h.wantSavedAlbums("Creator", "Mango Beats", "Zebra Songs", "Apple Tunes")
}

func TestAlbumTitleNamesItsArtists(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(albumsLoaded{albums: []spotify.SavedAlbum{
		{Album: spotify.Album{ID: "t", URI: "spotify:album:t", Name: "Tropicália 2", Artists: []string{"Caetano Veloso", "Gilberto Gil"}}},
		{Album: spotify.Album{ID: "n", URI: "spotify:album:n", Name: "Nameless"}},
	}})

	h.wantView("Tropicália 2 - Caetano Veloso, Gilberto Gil")
	if a, _ := h.albumTab().selectedAlbum(); a.artist != "Caetano Veloso, Gilberto Gil" {
		t.Fatalf("artist = %q, want both artists", a.artist)
	}

	h.send(key("down"))
	h.wantView("Nameless")
	h.dontWantView("Nameless -")
}

func TestAlbumSortKeepsTheSelectedAlbum(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(keys("down", "down")...)

	h.send(keys("s", "s")...)

	if got := h.selectedSavedAlbum(); got != "Clube da Esquina" {
		t.Fatalf("selected = %q, want Clube da Esquina", got)
	}
	if got := len(h.api.requestsFor("a2")); got != 3 {
		t.Fatalf("song requests for a2 = %d, sorting reloaded the songs", got)
	}
}

func TestAlbumSortKeyWorksFromTheSongs(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("tab", "s")...)

	h.wantSavedAlbums("Recently added", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	if got := h.selectedSavedAlbum(); got != "Abbey Road" {
		t.Fatalf("selected = %q, want Abbey Road", got)
	}
}

func TestPlayingAnAlbumMovesItToTheTop(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("down", "enter")...)

	h.wantSavedAlbums("Recents", "Kind of Blue", "Abbey Road", "Clube da Esquina")
	if got := h.selectedSavedAlbum(); got != "Kind of Blue" {
		t.Fatalf("selected = %q, want Kind of Blue", got)
	}
}

func TestPlayingAnAlbumSongMovesTheAlbumToTheTop(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("down", "down")...)
	h.send(keys("tab", "down", "enter")...)

	h.wantPlay(0, "spotify:album:a2", 1)
	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")
	if got := h.selectedSavedAlbum(); got != "Clube da Esquina" {
		t.Fatalf("selected = %q, want Clube da Esquina", got)
	}
	if got := h.selectedAlbumSong(); got.Name != "Track 2" {
		t.Fatalf("selected song = %q, want Track 2", got.Name)
	}
}

func TestPlayingAnAlbumKeepsOtherOrders(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("s", "down", "enter")...)

	h.wantPlay(0, "spotify:album:a1", 0)
	h.wantSavedAlbums("Recently added", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	if h.albumTab().history.AlbumLastPlayed("spotify:album:a1").IsZero() {
		t.Fatal("the play was not recorded")
	}
}

func TestFailedAlbumPlayIsNotRecorded(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newAlbumsHarness(t, api)

	h.send(keys("down", "enter")...)

	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	if got := h.albumTab().history.AlbumLastPlayed("spotify:album:a1"); !got.IsZero() {
		t.Fatalf("a failed play was recorded at %v", got)
	}
}

func TestPollingRecordsAlbumsPlayedElsewhere(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:album:a2", "album"
	h := newAlbumsHarness(t, api)

	h.send(pollPlayback{})
	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")

	api.set(func(f *fakeAPI) { f.playing = "spotify:album:a1" })
	h.send(pollPlayback{})
	h.wantSavedAlbums("Recents", "Kind of Blue", "Clube da Esquina", "Abbey Road")

	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:playlist:p0", "" })
	h.send(pollPlayback{})
	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:show:s0", "show" })
	h.send(pollPlayback{})
	h.wantSavedAlbums("Recents", "Kind of Blue", "Clube da Esquina", "Abbey Road")
	for _, uri := range []libspotify.URI{"spotify:playlist:p0", "spotify:show:s0"} {
		if got := h.albumTab().history.AlbumLastPlayed(uri); !got.IsZero() {
			t.Fatalf("%s was recorded as an album at %v", uri, got)
		}
	}

	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:album:a0", "album" })
	h.send(pollPlayback{})
	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
}

func TestPollAfterAnAlbumPlaySeesTheOtherDevice(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:album:a2", "album"
	h := newAlbumsHarness(t, api)
	h.send(pollPlayback{})
	h.send(key("enter"))
	h.wantSavedAlbums("Recents", "Abbey Road", "Clube da Esquina", "Kind of Blue")

	h.send(pollPlayback{})

	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")
}

func TestPausedAlbumsAreNotRecorded(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:album:a2", "album"
	api.paused = true
	h := newAlbumsHarness(t, api)

	h.send(pollPlayback{})

	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
}

func TestFailedPollKeepsTheCurrentAlbum(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:album:a0", "album"
	h := newAlbumsHarness(t, api)
	h.send(pollPlayback{})
	played := h.albumTab().history.AlbumLastPlayed("spotify:album:a0")
	if played.IsZero() {
		t.Fatal("the first poll did not record the album")
	}

	api.set(func(f *fakeAPI) { f.playerStatus = http.StatusInternalServerError })
	h.send(pollPlayback{})
	api.set(func(f *fakeAPI) { f.playerStatus = 0 })
	h.send(pollPlayback{})

	if got := h.albumTab().history.AlbumLastPlayed("spotify:album:a0"); !got.Equal(played) {
		t.Fatalf("last played = %v after a failed poll, want %v: the same album was recorded again", got, played)
	}
}

func TestPollingBeforeTheAlbumsTabOpensIsKept(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:album:a1", "album"
	h := newHarnessWith(t, api, 120, 40)

	h.send(pollPlayback{})
	h.openAlbumsTab()

	h.wantSavedAlbums("Recents", "Kind of Blue", "Abbey Road", "Clube da Esquina")
}

func TestAlbumsStartedFromTheArtistsTabAreRecorded(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(key("enter"))
	seq := h.albumTab().playback.seq

	h.send(artistPlaybackStarted{seq: seq, name: "Kind of Blue", artist: "spotify:artist:ar0", uri: "spotify:album:a1"})

	h.wantSavedAlbums("Recents", "Kind of Blue", "Abbey Road", "Clube da Esquina")
	h.wantView("⏸ Abbey Road")
	played := h.albumTab().history.AlbumLastPlayed("spotify:album:a1")
	if played.IsZero() {
		t.Fatal("the album started from the artists tab was not recorded")
	}
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:a1"}})
	if got := h.albumTab().history.AlbumLastPlayed("spotify:album:a1"); !got.Equal(played) {
		t.Fatalf("the poll of the album the artists tab started recorded it again at %v, want %v", got, played)
	}
}

func TestArtistsStartedFromTheArtistsTabAreNotAlbums(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(key("enter"))
	seq := h.albumTab().playback.seq

	h.send(artistPlaybackStarted{seq: seq, name: "Radiohead", artist: "spotify:artist:ar0", uri: "spotify:artist:ar0"})
	h.send(artistPlaybackFailed{seq: seq, err: errors.New("artist failure")})

	h.wantSavedAlbums("Recents", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	h.wantView("⏸ Abbey Road")
	h.dontWantView("Radiohead", "failure")
	if got := h.albumTab().history.AlbumLastPlayed("spotify:artist:ar0"); !got.IsZero() {
		t.Fatalf("the artist was recorded as an album at %v", got)
	}
}

func TestRecentlyPlayedAlbumsComeFirst(t *testing.T) {
	latest := ago(time.Hour)
	api := newFakeAPI()
	api.recent = []recentPlay{
		{"album", "spotify:album:a2", latest},
		{"playlist", "spotify:playlist:p0", ago(time.Minute)},
		{"album", "spotify:album:not-saved", ago(time.Minute)},
		{"", "", ago(time.Minute)},
		{"album", "spotify:album:a1", ago(2 * time.Hour)},
		{"album", "spotify:album:a2", ago(3 * time.Hour)},
	}
	h := newAlbumsHarness(t, api)

	h.wantSavedAlbums("Recents", "Clube da Esquina", "Kind of Blue", "Abbey Road")
	if got := h.selectedSavedAlbum(); got != "Clube da Esquina" {
		t.Fatalf("selected = %q, want the first album", got)
	}
	history := h.albumTab().history
	if got := history.AlbumLastPlayed("spotify:album:a2"); !got.Equal(latest) {
		t.Fatalf("a2 last played = %v, want the latest play", got)
	}
	if got := history.AlbumLastPlayed("spotify:album:not-saved"); !got.IsZero() {
		t.Fatalf("an album that is not saved is in the history: %v", got)
	}
}

func TestAlbumsTabDoesNotPoll(t *testing.T) {
	api := newFakeAPI()
	m := newModelWith(api, 120, 40)
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.pollEvery = time.Minute
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m, holdTimers: true}
	h.run(m.Init())
	h.openAlbumsTab()
	h.timers = nil

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:a0"}})

	if len(h.timers) != 1 {
		t.Fatalf("timers after one poll = %v, want one", h.timers)
	}
	if _, ok := h.timers[0].(pollPlayback); !ok {
		t.Fatalf("timer = %#v, want the next poll", h.timers[0])
	}
}

func TestAlbumOrderAndHistoryAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))
	h.openAlbumsTab()

	h.send(keys("down", "down", "s", "enter")...)
	h.wantSavedAlbums("Recently added", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:a1"}})

	var saved struct {
		Albums struct {
			Order      string               `json:"order"`
			LastPlayed map[string]time.Time `json:"lastPlayed"`
		} `json:"albums"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Albums.Order != "recently-added" || saved.Albums.LastPlayed["spotify:album:a1"].IsZero() || saved.Albums.LastPlayed["spotify:album:a2"].IsZero() {
		t.Fatalf("saved history = %s", b)
	}

	again := newStoredHarness(t, api, library.NewStore(path))
	again.openAlbumsTab()
	again.wantSavedAlbums("Recently added", "Abbey Road", "Kind of Blue", "Clube da Esquina")
	again.send(keys("s", "s", "s")...)
	again.wantSavedAlbums("Recents", "Kind of Blue", "Clube da Esquina", "Abbey Road")
}

func TestAlbumsShareTheLibraryFileWithTheOtherTabs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))

	h.send(key("s"))
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	h.openAlbumsTab()
	h.send(keys("s", "s")...)
	h.wantSavedAlbums("Alphabetical", "Abbey Road", "Clube da Esquina", "Kind of Blue")

	again := newStoredHarness(t, api, library.NewStore(path))
	again.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	again.openAlbumsTab()
	again.wantSavedAlbums("Alphabetical", "Abbey Road", "Clube da Esquina", "Kind of Blue")
	if got := again.podcasts().order(); got != library.OrderRecents {
		t.Fatalf("podcast order = %q, want the default", got)
	}
}

func TestAlbumHistorySaveErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "library.json")
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.openAlbumsTab()

	h.wantView("Fail to save the album history", "Abbey Road")
}

func TestAlbumHistoryReadErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.openAlbumsTab()

	h.wantView("Fail to read the album history", "Abbey Road")
}

func TestUnsavedAlbumsLeaveTheHistory(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:gone"}})
	if h.albumTab().history.AlbumLastPlayed("spotify:album:gone").IsZero() {
		t.Fatal("an album played elsewhere was not recorded")
	}

	h.openAlbumsTab()

	if got := h.albumTab().history.AlbumLastPlayed("spotify:album:gone"); !got.IsZero() {
		t.Fatalf("an album that is no longer saved is still in the history: %v", got)
	}
}

func TestAlbumFilterPlaysTheMatchingAlbum(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "k", "enter")...)
	if got := h.savedAlbumTitles(); !reflect.DeepEqual(got, []string{"Kind of Blue"}) {
		t.Fatalf("albums = %v, want [Kind of Blue]", got)
	}
	h.wantView("So What - Miles Davis")

	h.send(key("enter"))
	h.wantPlay(0, "spotify:album:a1", 0)

	h.send(keys("left", "left", "left")...)
	if got := h.visibleTitles(); len(got) != len(testPlaylists) {
		t.Fatalf("playlists after filtering the albums = %v, want all of them", got)
	}
}

func TestArrowKeysMoveTheAlbumFilterCursor(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "k", "b", "left", "left", "right", "x")...)

	if got := h.player().activeTab; got != albumsTab {
		t.Fatalf("activeTab = %d, want %d", got, albumsTab)
	}
	if got := h.albumTab().list.FilterValue(); got != "kxb" {
		t.Fatalf("filter = %q, want %q", got, "kxb")
	}
}

func TestEnterWhileFilteringAlbumsDoesNotPlay(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "k", "enter")...)

	h.wantPlays(0)
	if got := h.albumTab().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	h.send(key("enter"))
	h.wantPlay(0, "spotify:album:a1", 0)
}

func TestTabWhileFilteringAlbumsAppliesTheFilter(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "k", "tab")...)

	if got := h.albumTab().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if !h.albumTab().leftFocus {
		t.Fatal("tab while filtering moved the focus away from the albums")
	}
}

func TestPlaysWhileTypingSortAlbumsAfterTheFilterCloses(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "a")...)
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:a2"}})
	if got := h.savedAlbumTitles(); !reflect.DeepEqual(got, []string{"Abbey Road", "Clube da Esquina"}) {
		t.Fatalf("albums while typing = %v, want them untouched", got)
	}
	if got := h.albumTab().list.FilterValue(); got != "a" {
		t.Fatalf("filter = %q, want a", got)
	}

	h.send(key("esc"))
	h.wantSavedAlbums("Recents", "Clube da Esquina", "Abbey Road", "Kind of Blue")
}

func TestAlbumSortKeepsTheAppliedFilter(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(keys("/", "a", "enter")...)
	h.send(key("down"))
	h.wantSavedAlbums("Recents", "Abbey Road", "Clube da Esquina")

	h.send(keys("s", "s", "s")...)

	h.wantSavedAlbums("Creator", "Clube da Esquina", "Abbey Road")
	if got := h.selectedSavedAlbum(); got != "Clube da Esquina" {
		t.Fatalf("selected = %q, want Clube da Esquina", got)
	}
	if got := h.albumTab().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if got := h.albumTab().list.FilterValue(); got != "a" {
		t.Fatalf("filter = %q, want a", got)
	}
}

func TestSortKeyWhileFilteringAlbumsIsTyped(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(keys("/", "s")...)
	if got := h.albumTab().list.Title; got != "Recents" {
		t.Fatalf("typing s in the filter changed the order to %q", got)
	}
	if got := h.albumTab().list.FilterValue(); got != "s" {
		t.Fatalf("filter = %q, want s", got)
	}
}

func TestAlbumEmojiNamesArePrintable(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())

	h.send(albumsLoaded{albums: []spotify.SavedAlbum{
		{Album: spotify.Album{ID: "e0", URI: "spotify:album:e0", Name: "Café ☕️ com 👋🏽", Artists: []string{"Banda 🧑🏿‍🎤"}}},
		{Album: spotify.Album{ID: "e1", URI: "spotify:album:e1"}},
	}})

	h.wantView("Café ☕ com 👋🏽", "Café ☕ com 👋🏽 - Banda 🧑🏿🎤", "e1")
}

func TestAlbumItemsAreDefaultItems(t *testing.T) {
	var li list.Item = albumItem{title: "Abbey Road"}
	if _, ok := li.(list.DefaultItem); !ok {
		t.Fatal("albumItem does not work with the default delegate")
	}
}

func TestTabMovesTheKeysToTheAlbumSongs(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.wantView("tab songs")

	h.send(keys("tab", "down", "down")...)
	if got := h.selectedAlbumSong(); got.Name != "Her Majesty" {
		t.Fatalf("selected song = %q, want Her Majesty", got.Name)
	}
	if got := h.selectedSavedAlbum(); got != "Abbey Road" {
		t.Fatalf("selected album = %q, want Abbey Road", got)
	}
	h.wantView("tab albums")

	h.send(keys("tab", "down")...)
	if got := h.selectedSavedAlbum(); got != "Kind of Blue" {
		t.Fatalf("selected album = %q, want Kind of Blue", got)
	}
	h.wantView("tab songs")
}

func TestAlbumSongsFollowTheFocus(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	h.send(key("down"))
	h.send(key("up"))
	highlight := selectedSongStyle.Render("x")
	marker := highlight[:strings.Index(highlight, "x")]

	for _, focused := range []bool{true, false} {
		h.send(key("tab"))
		if len(h.albumTab().songs) != 2 {
			t.Fatalf("songs loaded for %d albums, want 2", len(h.albumTab().songs))
		}
		for id, songs := range h.albumTab().songs {
			if got := strings.Contains(songs.list.View(), marker); got != focused {
				t.Fatalf("songs of %s highlight the cursor = %v, want %v", id, got, focused)
			}
		}
	}
}

func TestStatusFollowsTheNextAlbumSong(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.send(key("enter"))
	h.wantView("⏸ Abbey Road")

	api.playsElsewhere("spotify:album:a0", "album", albumTrackURI("Something"), "Something")
	h.settleAlbums()
	h.send(pollPlayback{})

	h.wantView("⏸ Something")
}

func TestStatusKeepsTheAlbumSongItStarted(t *testing.T) {
	api := newFakeAPI()
	h := newAlbumsHarness(t, api)
	h.send(keys("tab", "down", "enter")...)

	api.playsElsewhere("spotify:album:a0", "album", albumTrackURI("Something"), "Something - Remastered")
	h.settleAlbums()
	h.send(pollPlayback{})

	h.wantView("⏸ Something")
	h.dontWantView("Remastered")
}

func TestNowPlayingShowsWhatTheAlbumsTabStarts(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	published := h.recordNowPlaying()

	h.send(key("enter"))
	if got, want := lastInfo(t, published), (media.Info{Title: "Abbey Road", Playing: true}); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}

	h.send(keys("tab", "down", "enter")...)
	if got, want := lastInfo(t, published), (media.Info{ID: albumTrackURI("Something"), Title: "Something", Artist: "The Beatles", Duration: 182 * time.Second, Playing: true}); got != want {
		t.Fatalf("now playing = %+v, want %+v", got, want)
	}
}

func TestSpacePausesFromTheAlbumsTab(t *testing.T) {
	h := newAlbumsHarness(t, playingOnPhone())

	h.send(key("space"))

	h.wantPauses("")
	h.wantPlays(0)
	h.wantView("▶ So What")
}

func TestAlbumSongStartedDuringAToggleKeepsItsStatus(t *testing.T) {
	h := newAlbumsHarness(t, playingOnPhone())
	h.send(keys("tab", "down")...)

	model, toggle := h.model.Update(key("space"))
	h.model = model
	h.send(key("enter"))
	h.run(toggle)

	h.wantView("⏸ Something")
	h.dontWantView("▶")
}

func TestAlbumPollSentBeforeAPlayKeepsTheStatus(t *testing.T) {
	h := newAlbumsHarness(t, newFakeAPI())
	plays := *h.albumTab().plays
	old := spotify.NowPlaying{Context: "spotify:album:a0", Item: "spotify:track:old", Name: "Old Song"}

	h.send(key("enter"))
	h.settleAlbums()
	h.send(playbackPolled{plays: plays, playing: old})
	h.wantView("⏸ Abbey Road")
	h.dontWantView("Old Song")

	h.send(playbackPolled{plays: plays + 1, playing: old})
	h.wantView("⏸ Old Song")
}
