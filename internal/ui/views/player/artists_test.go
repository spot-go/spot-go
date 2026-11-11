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
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

func (f *fakeAPI) followedArtistRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.artistRequests
}

func (h *harness) artists() artistModel {
	return h.player().tabs[artistsTab].content.(artistModel)
}

func (h *harness) selectedArtist() string {
	a, _ := h.artists().list.SelectedItem().(artistItem)
	return a.title
}

func (h *harness) artistTitles() []string {
	var titles []string
	for _, li := range h.artists().list.VisibleItems() {
		titles = append(titles, li.(artistItem).title)
	}
	return titles
}

func (h *harness) wantArtists(label string, titles ...string) {
	h.t.Helper()

	if got := h.artistTitles(); !reflect.DeepEqual(got, titles) {
		h.t.Fatalf("artists = %v, want %v", got, titles)
	}
	if got := h.artists().list.Title; got != label {
		h.t.Fatalf("list title = %q, want %q", got, label)
	}
	h.wantView(label)
}

func (h *harness) discographyNames() []string {
	h.t.Helper()

	d := h.artists().selectedDiscography()
	if d == nil || d.list == nil {
		h.t.Fatalf("albums are not loaded:\n%s", h.view())
	}
	var names []string
	for _, a := range listedDiscography(d.list) {
		names = append(names, a.Name)
	}
	return names
}

func (h *harness) selectedDiscographyAlbum() string {
	a, _ := h.artists().selectedAlbum()
	return a.Name
}

func (h *harness) discographyCursorIsGreen() bool {
	h.t.Helper()

	d := h.artists().selectedDiscography()
	if d == nil || d.list == nil {
		h.t.Fatalf("albums are not loaded:\n%s", h.view())
	}
	row := func(focused bool) string {
		var b bytes.Buffer
		discographyDelegate{focused: focused}.Render(&b, *d.list, d.list.Index(), d.list.SelectedItem())
		return b.String()
	}
	green, plain := row(true), row(false)
	if green == plain {
		h.t.Fatalf("the focused row looks like the unfocused one: %q", green)
	}
	view := h.model.View().Content
	switch {
	case strings.Contains(view, green):
		return true
	case strings.Contains(view, plain):
		return false
	}
	h.t.Fatalf("the selected album row %q is not in the view:\n%s", ansi.Strip(plain), view)
	return false
}

func (h *harness) wantArtistPlay(i int, artist string) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	got := plays[i]
	want := map[string]any{"context_uri": artist}
	if got.deviceID != testDeviceID || !reflect.DeepEqual(got.body, want) {
		h.t.Fatalf("play %d = device %q body %v, want device %q body %v", i, got.deviceID, got.body, testDeviceID, want)
	}
}

func (h *harness) settleArtists() {
	m := h.player()
	artists := m.tabs[artistsTab].content.(artistModel)
	artists.playback.since = time.Time{}
	m.tabs[artistsTab].content = artists
	h.model = m
}

func newArtistHarness(t *testing.T, api *fakeAPI) *harness {
	t.Helper()

	h := newHarnessWith(t, api, 120, 42)
	h.send(keys("right", "right")...)
	return h
}

func manyAlbumNames(from, to int) []string {
	var names []string
	for i := from; i <= to; i++ {
		names = append(names, fmt.Sprintf("Album %d", i))
	}
	return names
}

func TestArtistsLoadWhenTheTabOpens(t *testing.T) {
	api := newFakeAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("right"))
	if got := api.followedArtistRequests(); got != 0 {
		t.Fatalf("artists were requested %d times before the tab opened", got)
	}

	h.send(key("right"))
	h.wantView("Radiohead", "Portishead", "Björk", "3 artists")
	h.wantArtists("Recents", "Radiohead", "Portishead", "Björk")

	h.send(keys("left", "right")...)
	if got := api.followedArtistRequests(); got != 1 {
		t.Fatalf("artists were requested %d times, want 1", got)
	}
	if h.artistLoads != 1 {
		t.Fatalf("artists loaded %d times, want 1", h.artistLoads)
	}
	if got, want := api.requestsFor("ar0"), []string{"ar0@0", "ar0@10", "ar0@20"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
}

func TestAllPagesOfArtistsLoad(t *testing.T) {
	api := newFakeAPI()
	api.artists = nil
	for i := range 70 {
		api.artists = append(api.artists, artistJSON(fmt.Sprintf("x%d", i), fmt.Sprintf("Artist %d", i)))
	}
	h := newArtistHarness(t, api)

	if got := len(h.artists().list.Items()); got != 70 {
		t.Fatalf("artists = %d, want 70", got)
	}
	if got := api.followedArtistRequests(); got != 2 {
		t.Fatalf("artist pages requested = %d, want 2", got)
	}
}

func TestAlbumsOfTheFirstArtistLoad(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.wantView("Radiohead (30 of 34)", "> Album 1 ", "Album        2030", "Album 29", "2002", "30 albums")
	h.dontWantView("Album 30")
	if got, want := h.discographyNames(), manyAlbumNames(1, 30); !reflect.DeepEqual(got, want) {
		t.Fatalf("albums = %v, want %v", got, want)
	}
}

func TestAlbumsShowTheirTypeAndYear(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(key("down"))

	h.wantView("Portishead", "3 albums", "Dummy", "Album        1994", "Glory Box", "Single       1995", "Album        1997")
	h.dontWantView(" of 3")
	if got, want := h.discographyNames(), []string{"Dummy", "Portishead", "Glory Box"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("albums = %v, want them in the API order %v", got, want)
	}
}

func TestNoArtists(t *testing.T) {
	api := newFakeAPI()
	api.artists = nil
	h := newArtistHarness(t, api)

	h.wantView("No artists", "You don't follow any artists.", "then press r")
	api.set(func(f *fakeAPI) { f.artists = testArtistsJSON() })
	h.send(key("r"))

	h.wantArtists("Recents", "Radiohead", "Portishead", "Björk")
	if h.artistLoads != 2 {
		t.Fatalf("artists loaded %d times, want 2", h.artistLoads)
	}
}

func TestArtistsLoadErrorAndRetry(t *testing.T) {
	api := newFakeAPI()
	api.artistsStatus = http.StatusInternalServerError
	h := newArtistHarness(t, api)

	h.wantView("Fail to load artists", "request failed", "Press r to try again")
	h.send(keys("down", "enter", "s", "tab", "enter")...)
	h.wantPlays(0)

	api.set(func(f *fakeAPI) { f.artistsStatus = 0 })
	h.send(key("r"))
	h.wantArtists("Recents", "Radiohead", "Portishead", "Björk")
	if got := api.followedArtistRequests(); got != 2 {
		t.Fatalf("artists were requested %d times, want 2", got)
	}
}

func TestArtistsLoadedWhileOnAnotherTab(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(key("right"))
	model, cmd := h.model.Update(key("right"))
	h.model = model

	h.send(key("left"))
	h.run(cmd)
	h.send(key("right"))

	if h.artists().state != stateArtistsLoaded {
		t.Fatalf("state = %v, want loaded", h.artists().state)
	}
	h.wantView("Radiohead", "Album 1")
}

func TestResizeKeepsTheSelectedArtist(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(key("down"))

	for _, size := range [][2]int{{100, 30}, {80, 24}, {120, 40}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	}
	h.send(key("left"))
	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	h.send(key("right"))

	if got := h.selectedArtist(); got != "Portishead" {
		t.Fatalf("selected = %q, want Portishead", got)
	}
	if got, want := tabSize(h.artists()), h.player().contentDimension(); got != want {
		t.Fatalf("artists tab size = %+v, want %+v", got, want)
	}
	if h.artistLoads != 1 {
		t.Fatalf("artists loaded %d times, want 1", h.artistLoads)
	}
}

func TestMovingTheArtistCursorWaitsBeforeLoadingAlbums(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)
	h.holdTimers = true

	h.send(key("down"))
	if got := api.requestsFor("ar1"); len(got) != 0 {
		t.Fatalf("albums were requested right after the cursor moved: %v", got)
	}
	h.wantView("Loading albums...")
	if want := []time.Duration{songsLoadDelay}; !reflect.DeepEqual(h.delays, want) {
		t.Fatalf("delays = %v, want %v", h.delays, want)
	}

	h.holdTimers = false
	h.send()
	if got, want := api.requestsFor("ar1"), []string{"ar1@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
	h.wantView("Dummy")
}

func TestFastScrollingLoadsOnlyTheSelectedArtist(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)

	h.send(keys("down", "down")...)

	if got := api.requestsFor("ar1"); len(got) != 0 {
		t.Fatalf("album requests for ar1 = %v, want none for an artist scrolled past", got)
	}
	if got, want := api.requestsFor("ar2"), []string{"ar2@0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests for ar2 = %v, want %v", got, want)
	}
}

func TestAlbumsAreLoadedOncePerArtist(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)

	for _, k := range []string{"down", "up", "down", "up"} {
		h.send(key(k))
	}

	if got := api.requestsFor("ar0"); len(got) != 3 {
		t.Fatalf("album requests for ar0 = %v, want the first 3 pages", got)
	}
	if got := api.requestsFor("ar1"); len(got) != 1 {
		t.Fatalf("album requests for ar1 = %v, want 1", got)
	}
}

func TestAlbumRequestForAnotherArtistIsIgnored(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)

	h.send(discographyRequested{artistID: "ar2"})

	if got := api.requestsFor("ar2"); len(got) != 0 {
		t.Fatalf("album requests for an artist that is not selected = %v, want none", got)
	}
}

func TestAlbumsLoadAsTheCursorNearsTheEnd(t *testing.T) {
	api := newFakeAPI()
	api.artistAlbums["ar0"] = manyArtistAlbumsJSON(100)
	h := newArtistHarness(t, api)
	h.wantView("Radiohead (30 of 100)", "30 albums")

	h.send(keys("tab", "down")...)
	if got, want := api.requestsFor("ar0"), []string{"ar0@0", "ar0@10", "ar0@20", "ar0@30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
	for range 9 {
		h.send(key("down"))
	}
	if got := api.requestsFor("ar0"); len(got) != 4 {
		t.Fatalf("albums a full page ahead of the cursor loaded more pages: %v", got)
	}

	h.send(key("down"))
	h.wantView("Radiohead (50 of 100)", "50 albums")

	for range 3 {
		h.send(key("end"))
	}
	h.wantView("100 albums", "Album 100", "1931")
	h.dontWantView("of 100")
	h.send(keys("up", "down")...)
	if got := api.requestsFor("ar0"); len(got) != 10 {
		t.Fatalf("album requests = %v, want the 10 pages once", got)
	}
	if got, want := h.discographyNames(), manyAlbumNames(1, 100); !reflect.DeepEqual(got, want) {
		t.Fatalf("albums = %v, want %v", got, want)
	}
}

func TestAlbumsFillTheVisibleRows(t *testing.T) {
	api := newFakeAPI()
	api.artistAlbums["ar0"] = manyArtistAlbumsJSON(100)
	h := newArtistHarness(t, api)

	rows := h.artists().discographies["ar0"].list.Paginator.PerPage
	if got := len(h.discographyNames()); got < rows {
		t.Fatalf("albums = %d, want at least the %d visible rows", got, rows)
	}

	h.send(tea.WindowSizeMsg{Width: 120, Height: 80})
	rows = h.artists().discographies["ar0"].list.Paginator.PerPage
	if got := len(h.discographyNames()); got < rows {
		t.Fatalf("albums after the window grew = %d, want at least the %d visible rows", got, rows)
	}
	h.wantView("Album 50")
}

func TestShortAlbumPagesLoadUntilTheListIsLongEnough(t *testing.T) {
	api := newFakeAPI()
	api.totals["ar1"] = 30
	h := newArtistHarness(t, api)

	h.send(key("down"))

	if got, want := api.requestsFor("ar1"), []string{"ar1@0", "ar1@3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
	h.wantView("3 albums")
	h.dontWantView("of 30")
}

func TestDuplicateAlbumsAcrossPagesAreDropped(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)
	h.send(key("tab"))

	api.set(func(f *fakeAPI) {
		f.artistAlbums["ar0"] = append([]string{albumJSON("al-new", "New Album", "album", "2031", "year", 9, "Radiohead")}, f.artistAlbums["ar0"]...)
	})
	for range 10 {
		h.send(key("down"))
	}

	names := h.discographyNames()
	if len(names) != 34 {
		t.Fatalf("albums = %d, want 34", len(names))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("%q is listed twice", name)
		}
		seen[name] = true
	}
	if seen["New Album"] {
		t.Fatal("an album released after the first page was added in the middle of the list")
	}
}

func TestArtistWithoutAlbumsSaysSo(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("down", "down")...)

	h.wantView("Björk", "Björk has no albums, singles or compilations.")
	h.dontWantView("No albums")
}

func TestAlbumsLoadErrorAndRetry(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		api := newFakeAPI()
		api.songsStatus["ar0"] = status
		h := newArtistHarness(t, api)

		h.wantView("Fail to load albums", "request failed", "Press r to try again")

		api.set(func(f *fakeAPI) { delete(f.songsStatus, "ar0") })
		h.send(key("r"))
		h.wantView("Radiohead (30 of 34)", "Album 1")
		if got := api.followedArtistRequests(); got != 1 {
			t.Fatalf("artists were requested %d times, want r to reload only the albums", got)
		}
	}
}

func TestPartialAlbumFailureStopsLoadingMore(t *testing.T) {
	api := newFakeAPI()
	api.rateLimits["ar0"] = rateLimit{offset: "20", times: 10, retryAfter: "3600"}
	h := newArtistHarness(t, api)
	h.send(key("tab"))

	for range 10 {
		h.send(key("down"))
	}
	h.wantView("Radiohead (fail to load: API rate limit exceeded)", "Album 11")
	h.send(keys("down", "down", "end")...)
	if got, want := api.requestsFor("ar0"), []string{"ar0@0", "ar0@10", "ar0@20"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests after a failed page = %v, want %v", got, want)
	}

	api.set(func(f *fakeAPI) { delete(f.rateLimits, "ar0") })
	h.send(key("r"))
	if got := len(h.discographyNames()); got < 20 {
		t.Fatalf("albums after refreshing = %d, want at least 20", got)
	}
	h.dontWantView("fail to load")
}

func TestRefreshReloadsTheLoadedAlbums(t *testing.T) {
	api := newFakeAPI()
	api.artistAlbums["ar0"] = manyArtistAlbumsJSON(100)
	h := newArtistHarness(t, api)
	h.send(key("tab"))
	for range 5 {
		h.send(key("down"))
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 6" {
		t.Fatalf("selected = %q, want Album 6", got)
	}

	api.set(func(f *fakeAPI) {
		f.artistAlbums["ar0"] = append([]string{albumJSON("al-new", "New Album", "album", "2031", "year", 9, "Radiohead")}, f.artistAlbums["ar0"]...)
	})
	h.send(key("r"))

	pages := []string{"ar0@0", "ar0@10", "ar0@20", "ar0@30"}
	if got, want := api.requestsFor("ar0"), append(pages, pages...); !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
	if got := h.discographyNames(); len(got) != 40 || got[0] != "New Album" {
		t.Fatalf("albums after refreshing = %d starting with %q, want 40 starting with New Album", len(got), got[0])
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 6" {
		t.Fatalf("selected after refreshing = %q, want Album 6", got)
	}
	h.wantView("Radiohead (40 of 101)")
}

func TestRefreshDropsAlbumsThatShiftBetweenPages(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)

	first := h.artists().refreshDiscography("ar0")()
	api.set(func(f *fakeAPI) {
		f.artistAlbums["ar0"] = append([]string{albumJSON("al-new", "New Album", "album", "2031", "year", 9, "Radiohead")}, f.artistAlbums["ar0"]...)
	})
	h.send(first)

	if got, want := api.requestsFor("ar0"), []string{"ar0@0", "ar0@10", "ar0@20", "ar0@0", "ar0@10", "ar0@20", "ar0@30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("album requests = %v, want %v", got, want)
	}
	if got, want := h.discographyNames(), manyAlbumNames(1, 34); !reflect.DeepEqual(got, want) {
		t.Fatalf("albums after refreshing = %v, want %v with Album 10 listed once", got, want)
	}
	h.dontWantView("refreshing")
}

func TestRefreshKeepsTheAlbumsWhileLoading(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	a := h.artists()
	first := a.refreshDiscography("ar0")
	h.wantView("Radiohead (refreshing...)", "Album 1")

	second := a.refreshDiscography("ar0")
	h.run(first)
	h.wantView("Radiohead (refreshing...)")

	h.run(second)
	h.dontWantView("refreshing...")
	if got := len(h.discographyNames()); got != 30 {
		t.Fatalf("albums = %d, want 30", got)
	}
}

func TestFailedRefreshKeepsTheAlbums(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)

	api.set(func(f *fakeAPI) { f.songsStatus["ar0"] = http.StatusInternalServerError })
	h.send(key("r"))

	h.wantView("Radiohead (fail to load: request failed)", "Album 1")
	if got := len(h.discographyNames()); got != 30 {
		t.Fatalf("albums = %d, want 30", got)
	}
}

func TestRefreshRestartsAStalledAlbumLoad(t *testing.T) {
	api := newFakeAPI()
	release := make(chan struct{})
	defer close(release)
	api.block["ar0@20"] = release
	h := newArtistHarness(t, api)
	h.send(key("tab"))
	for range 10 {
		h.send(key("down"))
	}
	h.wantView("Radiohead (loading 20 of 34...)")

	h.send(key("r"))

	if got := len(h.discographyNames()); got != 34 {
		t.Fatalf("albums = %d, want 34", got)
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 11" {
		t.Fatalf("selected = %q, want Album 11", got)
	}
	h.dontWantView("loading", "refreshing")
}

func TestStaleAlbumResultsAreIgnored(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	a := h.artists()
	old := a.discographies["ar0"].gen
	refresh := a.refreshDiscography("ar0")

	stale := spotify.AlbumsPage{Items: []spotify.Album{{Name: "Old Album", URI: "spotify:album:old"}}, Next: 1, Total: 1, Last: true}
	h.send(discographyLoadFailed{artistID: "ar0", gen: old, err: errors.New("timeout")})
	h.send(discographyPageLoaded{artistID: "ar0", gen: old, page: stale})
	h.wantView("Radiohead (refreshing...)", "Album 1")
	h.dontWantView("Old Album", "timeout")

	h.run(refresh)
	h.dontWantView("refreshing", "fail to load", "Old Album")
	if got := len(h.discographyNames()); got != 30 {
		t.Fatalf("albums = %d, want 30", got)
	}
}

func TestRefreshingTwiceKeepsTheSelectedAlbum(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)
	h.send(key("tab"))
	for range 15 {
		h.send(key("down"))
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 16" {
		t.Fatalf("selected = %q, want Album 16", got)
	}

	api.set(func(f *fakeAPI) { f.rateLimits["ar0"] = rateLimit{offset: "10", times: 1, retryAfter: "3600"} })
	h.send(key("r"))
	h.wantView("Radiohead (fail to load: API rate limit exceeded)")
	h.send(key("r"))

	if got := len(h.discographyNames()); got != 34 {
		t.Fatalf("albums = %d, want 34", got)
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 16" {
		t.Fatalf("selected after refreshing twice = %q, want Album 16", got)
	}
}

func TestDiscographyRowsFillTheWidth(t *testing.T) {
	albums := []spotify.Album{
		{ID: "a0", URI: "spotify:album:a0", Name: "A very long album name that never fits in a narrow terminal 🎸", Type: "compilation", Released: time.Date(1997, 5, 21, 0, 0, 0, 0, time.UTC)},
		{ID: "a1", URI: "spotify:album:a1", Name: "Creep", Type: "single", Released: time.Date(1992, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "a2", URI: "spotify:album:a2", Name: "Mystery", Type: "appears_on_some_very_long_group"},
		{ID: "a3", URI: "spotify:album:a3", Type: "album", Released: time.Date(2003, 6, 1, 0, 0, 0, 0, time.UTC)},
	}
	types := []string{"Compilation", "Single", "Appears_on", "Album"}
	for _, focused := range []bool{false, true} {
		for width := 1; width <= 120; width++ {
			l := newList(discographyItems(albums), discographyDelegate{focused: focused})
			l.SetSize(width, 10)
			for i, li := range l.Items() {
				var b bytes.Buffer
				discographyDelegate{focused: focused}.Render(&b, l, i, li)
				row := b.String()
				if got := ansi.StringWidth(row); got != width {
					t.Fatalf("width %d: row %d is %d columns wide: %q", width, i, got, ansi.Strip(row))
				}
				if width < 16 {
					continue
				}
				plain := ansi.Strip(row)
				if year := discographyYear(albums[i]); !strings.HasSuffix(plain, year) {
					t.Fatalf("width %d: row %d does not end with the year %q: %q", width, i, year, plain)
				}
				if cursor := strings.HasPrefix(plain, "> "); cursor != (i == 0) {
					t.Fatalf("width %d: row %d shows the cursor = %v, want %v: %q", width, i, cursor, i == 0, plain)
				}
				hasType := strings.Contains(plain, types[i])
				wantType := width-2-1-discographyYearWidth-discographyTypeWidth-2 >= minDiscographyNameWidth
				if hasType != wantType {
					t.Fatalf("width %d: row %d shows the type = %v, want %v: %q", width, i, hasType, wantType, plain)
				}
				if i == 3 && !strings.Contains(plain, "a3") {
					t.Fatalf("width %d: an album without a name does not show its ID: %q", width, plain)
				}
			}
		}
	}
}

func TestDiscographyRowsAreGreenOnlyWhenFocused(t *testing.T) {
	albums := []spotify.Album{{Name: "Dummy", URI: "spotify:album:a"}, {Name: "Portishead", URI: "spotify:album:b"}}
	render := func(focused bool, index int) string {
		l := newList(discographyItems(albums), discographyDelegate{focused: focused})
		l.SetSize(60, 10)
		var b bytes.Buffer
		discographyDelegate{focused: focused}.Render(&b, l, index, l.Items()[index])
		return b.String()
	}

	if got, want := render(true, 0), selectedSongStyle.Render(ansi.Strip(render(true, 0))); got != want || got == ansi.Strip(got) {
		t.Fatalf("focused selected row = %q, want it green %q", got, want)
	}
	if got := render(false, 0); got != ansi.Strip(got) {
		t.Fatalf("selected row without focus = %q, want it plain", got)
	}
	if got := render(true, 1); got != ansi.Strip(got) {
		t.Fatalf("row without the cursor = %q, want it plain", got)
	}
}

func TestTabTurnsTheSelectedAlbumGreen(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	if h.discographyCursorIsGreen() {
		t.Fatal("the selected album is green while the artists are focused")
	}

	h.send(key("tab"))
	if !h.discographyCursorIsGreen() {
		t.Fatal("the selected album is not green after tab focused the albums")
	}

	h.send(key("tab"))
	if h.discographyCursorIsGreen() {
		t.Fatal("the selected album is still green after tab focused the artists")
	}
}

func TestAlbumsLoadedWhileFocusedAreGreen(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.holdTimers = true
	h.send(keys("down", "tab")...)

	h.holdTimers = false
	h.send()
	if got := h.selectedDiscographyAlbum(); got != "Dummy" {
		t.Fatalf("selected = %q, want Dummy", got)
	}
	if !h.discographyCursorIsGreen() {
		t.Fatal("albums loaded while focused are not green")
	}

	h.send(key("r"))
	if got := h.api.requestsFor("ar1"); len(got) != 2 {
		t.Fatalf("album requests = %v, want r to refresh them", got)
	}
	if !h.discographyCursorIsGreen() {
		t.Fatal("albums refreshed while focused are not green")
	}

	h.send(key("tab"))
	if h.discographyCursorIsGreen() {
		t.Fatal("refreshed albums are still green after tab focused the artists")
	}
}

func TestDiscographyType(t *testing.T) {
	for _, tt := range []struct{ kind, want string }{
		{"album", "Album"},
		{"single", "Single"},
		{"compilation", "Compilation"},
		{"ALBUM", "Album"},
		{"appears_on", "Appears_on"},
		{"épisode", "Épisode"},
		{"", ""},
	} {
		if got := discographyType(spotify.Album{Type: tt.kind}); got != tt.want {
			t.Errorf("discographyType(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestDiscographyYear(t *testing.T) {
	for _, tt := range []struct {
		released time.Time
		want     string
	}{
		{time.Date(1997, 5, 21, 0, 0, 0, 0, time.UTC), "1997"},
		{time.Date(1994, 1, 1, 0, 0, 0, 0, time.UTC), "1994"},
		{time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), ""},
		{time.Time{}, ""},
	} {
		if got := discographyYear(spotify.Album{Released: tt.released}); got != tt.want {
			t.Errorf("discographyYear(%v) = %q, want %q", tt.released, got, tt.want)
		}
	}
}

func TestAlbumsWithAnUnknownReleaseDateShowNoYear(t *testing.T) {
	api := newFakeAPI()
	api.artistAlbums["ar2"] = []string{
		albumJSON("al-unknown", "Unknown Date", "album", "0000", "year", 5, "Björk"),
		albumJSON("al-debut", "Debut", "album", "1993", "year", 11, "Björk"),
	}
	h := newArtistHarness(t, api)

	h.send(keys("down", "down")...)

	h.wantView("Debut", "Album        1993")
	var row string
	for _, line := range strings.Split(h.view(), "\n") {
		if strings.Contains(line, "Unknown Date") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("the album without a date is not listed:\n%s", h.view())
	}
	if fields := strings.Fields(strings.Trim(row, " │")); fields[len(fields)-1] != "Album" {
		t.Fatalf("the album without a date ends with %q, want no year after its type: %q", fields[len(fields)-1], row)
	}
}

func TestArtistsViewFitsTheWindow(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
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
	h.wantView("Portishead", "Dummy")
}

func TestArtistsFrameReachesTheRightEdge(t *testing.T) {
	api := newFakeAPI()
	api.artists = append(api.artists, artistJSON("ar3", "👩🏾‍❤️‍👩🏼 casal 🇧🇷 com um nome bem comprido 👋🏿"))
	api.artistAlbums["ar0"] = append([]string{albumJSON("al-emoji", "Tchau 👋🏽 e um título bem comprido", "single", "2026-11-01", "day", 1, "Radiohead")}, api.artistAlbums["ar0"]...)
	h := newArtistHarness(t, api)
	minWidth := lipgloss.Width(h.player().tabRow())

	for _, keys := range [][]string{nil, {"tab"}, {"tab", "down", "down", "down"}} {
		for _, k := range keys {
			h.send(key(k))
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

func TestArtistOrderLabelStaysOnTheFirstRow(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
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

func TestAlbumsListFollowsTheWindowSize(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(key("down"))

	for _, size := range [][2]int{{120, 40}, {80, 24}, {200, 60}, {60, 10}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		a := h.artists()
		for id, d := range a.discographies {
			if d.list.Width() != a.rightWidth() || d.list.Height() != a.listHeight() {
				t.Fatalf("%dx%d: albums of %s are %dx%d, want %dx%d", size[0], size[1], id, d.list.Width(), d.list.Height(), a.rightWidth(), a.listHeight())
			}
		}
	}
}

func TestArtistsViewDoesNotStartGoroutines(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	m := h.player()

	before := runtime.NumGoroutine()
	for range 200 {
		_ = m.View()
	}
	if after := runtime.NumGoroutine(); after-before > 20 {
		t.Fatalf("View started goroutines: %d before, %d after", before, after)
	}
}

func TestTabMovesTheKeysToTheAlbums(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.wantView("tab albums")

	h.send(keys("tab", "down")...)

	h.wantView("tab artists")
	if got := h.selectedArtist(); got != "Radiohead" {
		t.Fatalf("down on the albums moved the artist cursor to %q", got)
	}
	if got := h.selectedDiscographyAlbum(); got != "Album 2" {
		t.Fatalf("selected album = %q, want Album 2", got)
	}

	h.send(keys("tab", "down")...)
	if got := h.selectedArtist(); got != "Portishead" {
		t.Fatalf("down on the artists selected %q, want Portishead", got)
	}
}

func TestEnterOnAnArtistPlaysTheArtist(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(key("enter"))

	h.wantArtistPlay(0, "spotify:artist:ar0")
	h.wantPlays(1)
	h.wantView("⏸ Radiohead")
	if got := *h.artists().plays; got != 1 {
		t.Fatalf("plays = %d, want 1", got)
	}
}

func TestEnterOnADiscographyAlbumPlaysItFromTheStart(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(key("down"))
	h.send(keys("tab", "down", "down", "enter")...)

	h.wantPlay(0, "spotify:album:al-glory", 0)
	h.wantPlays(1)
	h.wantView("⏸ Glory Box")
}

func TestEnterOnTheAlbumsBeforeTheyLoadPlaysTheArtist(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.holdTimers = true

	h.send(keys("down", "tab", "enter")...)

	h.wantArtistPlay(0, "spotify:artist:ar1")
	h.wantView("⏸ Portishead")
}

func TestEnterOnAnArtistWithoutAlbumsPlaysTheArtist(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("down", "down", "tab", "enter")...)

	h.wantArtistPlay(0, "spotify:artist:ar2")
	h.wantView("⏸ Björk")
}

func TestArtistPlayShowsWhatItStarts(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	a, cmd := h.artists().play()
	if cmd == nil || a.playback.text != "Starting Radiohead..." || !a.playback.starting {
		t.Fatalf("play of the artist = %+v", a.playback)
	}

	a.leftFocus = false
	a, cmd = a.play()
	if cmd == nil || a.playback.text != "Starting Album 1..." || a.playback.seq != 2 {
		t.Fatalf("play of the album = %+v", a.playback)
	}
	if got := *a.plays; got != 2 {
		t.Fatalf("plays = %d, want 2", got)
	}
	h.wantPlays(0)
}

func TestArtistPlaybackErrorIsShown(t *testing.T) {
	api := newFakeAPI()
	api.playStatus = http.StatusForbidden
	h := newArtistHarness(t, api)

	h.send(key("enter"))
	h.wantView("Fail to play", "Premium required")

	h.send(keys("tab", "enter")...)
	h.wantView("Fail to play", "Premium required")
	h.wantPlays(2)
}

func TestStaleArtistPlaybackResultIsIgnored(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(keys("tab", "enter")...)

	h.send(artistPlaybackFailed{seq: 0, err: errors.New("old failure")})
	h.send(artistPlaybackStarted{seq: 0, name: "Old Album", artist: "spotify:artist:ar0", uri: "spotify:album:old"})

	h.wantView("⏸ Album 1")
	h.dontWantView("old failure", "Old Album")
}

func TestArtistsTabKeepsItsOwnPlaybackStatus(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(key("enter"))
	seq := h.artists().playback.seq
	h.send(playbackStarted{seq: seq, name: "Rock", uri: "spotify:playlist:p0"})
	h.send(episodePlaybackStarted{seq: seq, name: "News", show: "spotify:show:s0"})
	h.send(playbackFailed{seq: seq, err: errors.New("playlist failure")})
	h.send(episodePlaybackFailed{seq: seq, err: errors.New("episode failure")})
	h.wantView("⏸ Radiohead")

	h.send(key("left"))
	h.dontWantView("Radiohead")
}

func TestArtistStatusFollowsWhatSpotifyPicks(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)
	h.send(key("enter"))
	h.settleArtists()

	plays := *h.artists().plays
	h.send(playbackPolled{plays: plays, playing: spotify.NowPlaying{Context: "spotify:artist:ar0", Item: "spotify:track:airbag", Name: "Airbag"}})
	h.wantView("⏸ Airbag")

	h.send(playbackPolled{plays: plays - 1, playing: spotify.NowPlaying{Context: "spotify:artist:ar0", Item: "spotify:track:old", Name: "Old Song"}})
	h.wantView("⏸ Airbag")
	h.dontWantView("Old Song")

	h.send(playbackToggled{plays: plays, paused: true, name: "Airbag", item: "spotify:track:airbag"})
	h.wantView("▶ Airbag")

	h.send(playbackToggled{plays: plays - 1, name: "Old Song"})
	h.wantView("▶ Airbag")
	h.dontWantView("Old Song")
}

func TestArtistSortKeyCyclesTheOrders(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:artist:ar2"}})
	h.wantArtists("Recents", "Björk", "Radiohead", "Portishead")

	h.send(key("s"))
	h.wantArtists("Recently added", "Radiohead", "Portishead", "Björk")
	h.send(key("s"))
	h.wantArtists("Alphabetical", "Björk", "Portishead", "Radiohead")
	h.send(key("s"))
	h.wantArtists("Recents", "Björk", "Radiohead", "Portishead")
}

func TestSortKeyWorksFromTheAlbums(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("tab", "s", "s")...)

	h.wantArtists("Alphabetical", "Björk", "Portishead", "Radiohead")
	if got := h.selectedArtist(); got != "Radiohead" {
		t.Fatalf("selected = %q, want Radiohead", got)
	}
}

func TestArtistOrdersUseWhenTheArtistsWereFollowed(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(artistsLoaded{
		artists: []spotify.Artist{
			{ID: "ar0", URI: "spotify:artist:ar0", Name: "Radiohead"},
			{ID: "z", URI: "spotify:artist:z", Name: "Zebra Katz"},
			{ID: "ar2", URI: "spotify:artist:ar2", Name: "Björk"},
		},
		recent: []spotify.PlayedContext{{URI: "spotify:artist:ar2", At: ago(time.Hour)}},
	})

	h.wantArtists("Recents", "Zebra Katz", "Björk", "Radiohead")
	h.send(key("s"))
	h.wantArtists("Recently added", "Zebra Katz", "Radiohead", "Björk")
	h.send(key("s"))
	h.wantArtists("Alphabetical", "Björk", "Radiohead", "Zebra Katz")
	if _, ok := h.artists().history.Artists.Followed["spotify:artist:ar1"]; ok {
		t.Fatal("an unfollowed artist is still in the history")
	}
}

func TestRecentlyPlayedArtistsComeFirst(t *testing.T) {
	api := newFakeAPI()
	api.recent = []recentPlay{
		{"playlist", "spotify:playlist:p0", ago(time.Minute)},
		{"artist", "spotify:artist:ar1", ago(2 * time.Hour)},
		{"album", "spotify:album:al-dummy", ago(10 * time.Minute)},
		{"artist", "spotify:artist:ar2", ago(time.Hour)},
		{"artist", "spotify:artist:unfollowed", ago(time.Second)},
	}
	h := newArtistHarness(t, api)

	h.wantArtists("Recents", "Björk", "Portishead", "Radiohead")
}

func TestArtistSortKeepsTheSelectedArtist(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(keys("down", "down")...)

	h.send(keys("s", "s")...)

	if got := h.selectedArtist(); got != "Björk" {
		t.Fatalf("selected = %q, want Björk", got)
	}
}

func TestPlayingAnArtistMovesItToTheTop(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("down", "enter")...)

	h.wantArtists("Recents", "Portishead", "Radiohead", "Björk")
	if got := h.selectedArtist(); got != "Portishead" {
		t.Fatalf("selected = %q, want Portishead", got)
	}
}

func TestPlayingAnAlbumMovesItsArtistToTheTop(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(key("down"))
	h.send(keys("tab", "enter")...)

	h.wantPlay(0, "spotify:album:al-dummy", 0)
	h.wantArtists("Recents", "Portishead", "Radiohead", "Björk")
	h.wantView("⏸ Dummy")
	if got := h.artists().history.ArtistRecent("spotify:artist:ar1"); got.IsZero() {
		t.Fatal("playing an album of the artist did not record the artist")
	}
}

func TestPlayingKeepsOtherArtistOrders(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(key("s"))

	h.send(keys("down", "down", "enter")...)

	h.wantArtists("Recently added", "Radiohead", "Portishead", "Björk")
	if got := h.selectedArtist(); got != "Björk" {
		t.Fatalf("selected = %q, want Björk", got)
	}
}

func TestPollingRecordsArtistsPlayedElsewhere(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:artist:ar2", "artist"
	h := newArtistHarness(t, api)

	h.send(pollPlayback{})
	h.wantArtists("Recents", "Björk", "Radiohead", "Portishead")

	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:album:al-dummy", "album" })
	h.send(pollPlayback{})
	h.wantArtists("Recents", "Björk", "Radiohead", "Portishead")

	api.set(func(f *fakeAPI) { f.playing, f.playingType = "spotify:artist:ar1", "artist" })
	h.send(pollPlayback{})
	h.wantArtists("Recents", "Portishead", "Björk", "Radiohead")
}

func TestPollAfterAnArtistPlayIsNotRecordedAgain(t *testing.T) {
	api := newFakeAPI()
	h := newArtistHarness(t, api)
	h.send(keys("down", "enter")...)
	played := h.artists().history.ArtistRecent("spotify:artist:ar1")

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:artist:ar1"}})
	if got := h.artists().history.ArtistRecent("spotify:artist:ar1"); !got.Equal(played) {
		t.Fatalf("the poll of the artist spot-go started recorded it again at %v, want %v", got, played)
	}

	h.send(keys("tab", "enter")...)
	played = h.artists().history.ArtistRecent("spotify:artist:ar1")
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:album:al-dummy"}})
	if got := h.artists().history.ArtistRecent("spotify:artist:ar1"); !got.Equal(played) {
		t.Fatalf("the poll of the album spot-go started recorded the artist again at %v, want %v", got, played)
	}

	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:artist:ar1"}})
	if got := h.artists().history.ArtistRecent("spotify:artist:ar1"); !got.After(played) {
		t.Fatalf("the artist played elsewhere after its album was recorded at %v, want after %v", got, played)
	}
}

func TestArtistPlaybackResultsNameWhatPlays(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(key("down"))

	a, cmd := h.artists().play()
	if got, want := cmd(), (artistPlaybackStarted{seq: 1, name: "Portishead", artist: "spotify:artist:ar1", uri: "spotify:artist:ar1"}); got != want {
		t.Fatalf("artist play = %#v, want %#v", got, want)
	}

	a.leftFocus = false
	a.discographies["ar1"].list.Select(2)
	a, cmd = a.play()
	if got, want := cmd(), (artistPlaybackStarted{seq: 2, name: "Glory Box", artist: "spotify:artist:ar1", uri: "spotify:album:al-glory"}); got != want {
		t.Fatalf("album play = %#v, want %#v", got, want)
	}

	h.api.set(func(f *fakeAPI) { f.playStatus = http.StatusForbidden })
	_, cmd = a.play()
	failed, ok := cmd().(artistPlaybackFailed)
	if !ok || failed.seq != 3 || failed.err == nil {
		t.Fatalf("failed play = %#v, want artistPlaybackFailed with seq 3", failed)
	}
}

func TestPollingBeforeTheArtistsTabOpensIsKept(t *testing.T) {
	api := newFakeAPI()
	api.playing, api.playingType = "spotify:artist:ar1", "artist"
	h := newHarnessWith(t, api, 120, 40)

	h.send(pollPlayback{})
	h.send(keys("right", "right")...)

	h.wantArtists("Recents", "Portishead", "Radiohead", "Björk")
}

func TestArtistOrderAndHistoryAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))
	h.send(keys("right", "right")...)

	h.send(keys("down", "down", "s", "enter")...)
	h.wantArtists("Recently added", "Radiohead", "Portishead", "Björk")
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:artist:ar1"}})

	var saved struct {
		Artists struct {
			Order    string `json:"order"`
			Synced   bool   `json:"synced"`
			Followed map[string]struct {
				LastPlayed time.Time `json:"lastPlayed"`
			} `json:"followed"`
		} `json:"artists"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	a := saved.Artists
	if a.Order != "recently-added" || !a.Synced || len(a.Followed) != 3 || a.Followed["spotify:artist:ar2"].LastPlayed.IsZero() || a.Followed["spotify:artist:ar1"].LastPlayed.IsZero() {
		t.Fatalf("saved history = %s", b)
	}

	again := newStoredHarness(t, api, library.NewStore(path))
	again.send(keys("right", "right")...)
	again.wantArtists("Recently added", "Radiohead", "Portishead", "Björk")
	again.send(keys("s", "s")...)
	again.wantArtists("Recents", "Portishead", "Björk", "Radiohead")
}

func TestArtistOrderDoesNotChangeTheOtherTabs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := newFakeAPI()
	h := newStoredHarness(t, api, library.NewStore(path))

	h.send(key("s"))
	h.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	h.send(keys("right", "right")...)
	h.send(keys("s", "s")...)
	h.wantArtists("Alphabetical", "Björk", "Portishead", "Radiohead")

	again := newStoredHarness(t, api, library.NewStore(path))
	again.wantOrder("Alphabetical", "Blues", "Jazz", "Rock", "Samba")
	again.send(key("right"))
	again.wantShows("Recents", "Morning Briefing", "Go Weekly", "History Hour")
	again.send(key("right"))
	again.wantArtists("Alphabetical", "Björk", "Portishead", "Radiohead")
}

func TestArtistHistorySaveErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "library.json")
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.send(keys("right", "right")...)

	h.wantView("Fail to save the artist history")
}

func TestArtistHistoryReadErrorIsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.send(keys("right", "right")...)

	h.wantView("Fail to read the artist history", "Radiohead")
}

func TestUnfollowedArtistsLeaveTheHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	old := `{"artists":{"synced":true,"followed":{"spotify:artist:gone":{"lastPlayed":"2026-11-01T10:00:00Z"},"spotify:artist:ar1":{"lastPlayed":"2026-11-02T10:00:00Z"}}}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStoredHarness(t, newFakeAPI(), library.NewStore(path))

	h.send(keys("right", "right")...)

	followed := h.artists().history.Artists.Followed
	if _, ok := followed["spotify:artist:gone"]; ok {
		t.Fatal("an unfollowed artist is still in the history")
	}
	if got := h.artists().history.ArtistAdded("spotify:artist:ar0"); got.IsZero() {
		t.Fatal("an artist followed since the last sync has no follow date")
	}
	h.wantArtists("Recents", "Radiohead", "Björk", "Portishead")
}

func TestArtistFilterDoesNotChangeTheOtherTabs(t *testing.T) {
	h := newHarnessWith(t, newFakeAPI(), 120, 40)

	h.send(keys("/", "j", "enter")...)
	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Jazz"}) {
		t.Fatalf("playlists = %v, want [Jazz]", got)
	}

	h.send(keys("right", "right")...)
	h.send(keys("/", "b", "enter")...)
	if got := h.artistTitles(); !reflect.DeepEqual(got, []string{"Björk"}) {
		t.Fatalf("artists = %v, want [Björk]", got)
	}

	h.send(keys("left", "left")...)
	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Jazz"}) {
		t.Fatalf("playlists after filtering the artists = %v, want [Jazz]", got)
	}
	h.send(key("enter"))
	h.wantPlay(0, "spotify:playlist:p1", 0)
}

func TestArrowKeysMoveTheArtistFilterCursor(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("/", "r", "d", "left", "left", "right", "x")...)

	if got := h.player().activeTab; got != artistsTab {
		t.Fatalf("activeTab = %d, want %d", got, artistsTab)
	}
	if got := h.artists().list.FilterValue(); got != "rxd" {
		t.Fatalf("filter = %q, want %q", got, "rxd")
	}
	if !h.artists().capturesInput() {
		t.Fatal("the artists tab does not capture the keys while filtering")
	}
}

func TestEnterWhileFilteringArtistsDoesNotPlay(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("/", "b", "enter")...)

	h.wantPlays(0)
	if got := h.artists().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	h.send(key("enter"))
	h.wantArtistPlay(0, "spotify:artist:ar2")
}

func TestKeysWhileFilteringArtistsAreTyped(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("/", "r", "s", "d", "tab")...)

	if got := h.artists().list.Title; got != "Recents" {
		t.Fatalf("typing s in the filter changed the order to %q", got)
	}
	if got := h.artists().list.FilterValue(); got != "rsd" {
		t.Fatalf("filter = %q, want rsd", got)
	}
	if got := h.artists().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want tab to apply it", got)
	}
	if !h.artists().leftFocus {
		t.Fatal("tab while filtering moved the focus away from the artists")
	}
	if got := h.api.requestsFor("ar0"); len(got) != 3 {
		t.Fatalf("r while filtering refreshed the albums: %v", got)
	}
}

func TestPlaysWhileTypingSortArtistsAfterTheFilterCloses(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(keys("/", "d")...)
	h.send(playbackPolled{playing: spotify.NowPlaying{Context: "spotify:artist:ar1"}})
	if got := h.artistTitles(); !reflect.DeepEqual(got, []string{"Radiohead", "Portishead"}) {
		t.Fatalf("artists while typing = %v, want them untouched", got)
	}
	if got := h.artists().list.FilterValue(); got != "d" {
		t.Fatalf("filter = %q, want d", got)
	}

	h.send(key("esc"))
	h.wantArtists("Recents", "Portishead", "Radiohead", "Björk")
}

func TestArtistSortKeepsTheAppliedFilter(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())
	h.send(keys("/", "d", "enter")...)
	h.send(key("down"))
	h.wantArtists("Recents", "Radiohead", "Portishead")

	h.send(keys("s", "s")...)

	h.wantArtists("Alphabetical", "Portishead", "Radiohead")
	if got := h.selectedArtist(); got != "Portishead" {
		t.Fatalf("selected = %q, want Portishead", got)
	}
	if got := h.artists().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if got := h.artists().list.FilterValue(); got != "d" {
		t.Fatalf("filter = %q, want d", got)
	}
}

func TestArtistEmojiNamesArePrintable(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(artistsLoaded{artists: []spotify.Artist{{ID: "e0", URI: "spotify:artist:e0", Name: "Café ☕️ com 👋🏽"}, {ID: "e1", URI: "spotify:artist:e1"}}})

	h.wantView("Café ☕ com 👋🏽", "e1")
}

func TestArtistItemsAreDefaultItems(t *testing.T) {
	var li list.Item = artistItem{title: "Radiohead"}
	if _, ok := li.(list.DefaultItem); !ok {
		t.Fatal("artistItem does not work with the default delegate")
	}
}

func TestArtistTabIgnoresOtherTabsLoads(t *testing.T) {
	h := newArtistHarness(t, newFakeAPI())

	h.send(showsLoaded{shows: []spotify.SavedShow{{ID: "x", URI: "spotify:show:x", Name: "Other"}}})
	h.send(playlistsLoaded{playlists: []libspotify.SimplePlaylist{{ID: "p9", URI: "spotify:playlist:p9", Name: "Other"}}})
	h.send(showsLoadFailed{errors.New("shows failed")}, playlistsLoadFailed{errors.New("playlists failed")})

	h.wantArtists("Recents", "Radiohead", "Portishead", "Björk")
	h.dontWantView("shows failed", "playlists failed")
}
