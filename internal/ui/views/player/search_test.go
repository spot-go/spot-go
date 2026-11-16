package player

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var searchResponseKeys = map[string]string{
	"track":    "tracks",
	"artist":   "artists",
	"album":    "albums",
	"playlist": "playlists",
	"show":     "shows",
	"episode":  "episodes",
}

func (f *fakeAPI) search(r *http.Request) *http.Response {
	query := r.URL.Query()
	f.searches = append(f.searches, query)
	if f.searchStatus != 0 {
		return errorResponse(r, f.searchStatus, "search failed")
	}
	if query.Get("limit") != "10" || query.Get("market") != "from_token" || query.Get("q") == "" {
		return errorResponse(r, http.StatusBadRequest, "missing q, limit=10 or market=from_token")
	}
	offset, _ := strconv.Atoi(query.Get("offset"))
	var parts []string
	for kind := range strings.SplitSeq(query.Get("type"), ",") {
		all := f.searchItems[kind]
		page := all[min(offset, len(all)):min(offset+10, len(all))]
		parts = append(parts, fmt.Sprintf(`%q:{"items":[%s],"total":%d}`, searchResponseKeys[kind], strings.Join(page, ","), len(all)))
	}
	return response(r, http.StatusOK, "{"+strings.Join(parts, ",")+"}")
}

func searchAPI() *fakeAPI {
	api := newFakeAPI()
	api.searchItems = map[string][]string{
		"track": {
			albumTrackJSON("Come Together", "The Beatles", 259000, true),
			albumTrackJSON("Her Majesty", "The Beatles", 23000, false),
			"null",
		},
		"artist": {artistJSON("ar-beatles", "The Beatles")},
		"album":  {albumJSON("a0", "Abbey Road", "album", "1969-09-26", "day", 17, "The Beatles")},
		"playlist": {
			"null",
			`{"id":"pl-best","uri":"spotify:playlist:pl-best","name":"Beatles Best","owner":{"display_name":"Ana"}}`,
		},
		"show": {`{"id":"s0","uri":"spotify:show:s0","name":"Morning Briefing","publisher":"The Daily"}`},
	}
	return api
}

func (h *harness) searchTab() searchModel {
	return h.player().tabs[searchTab].content.(searchModel)
}

func (h *harness) searchFor(query string) {
	h.t.Helper()

	h.send(key("/"))
	for _, r := range query {
		h.send(key(string(r)))
	}
	h.send(key("enter"))
}

func (h *harness) searchResultNames() []string {
	var names []string
	r := h.searchTab().selectedResults()
	if r == nil || r.list == nil {
		return nil
	}
	for _, li := range r.list.Items() {
		names = append(names, li.(searchItem).name)
	}
	return names
}

func (h *harness) selectSearchCategory(name string) {
	h.t.Helper()

	if !h.searchTab().leftFocus {
		h.send(key("tab"))
	}
	h.send(keys("up", "up", "up", "up", "up", "up")...)
	for range searchCategories {
		if searchCategories[h.searchTab().category].name == name {
			h.send(key("enter"))
			return
		}
		h.send(key("down"))
	}
	h.t.Fatalf("no search category %q", name)
}

func (h *harness) wantBody(i int, want map[string]any) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	if got := plays[i]; got.deviceID != testDeviceID || !reflect.DeepEqual(got.body, want) {
		h.t.Fatalf("play %d = device %q body %v, want device %q body %v", i, got.deviceID, got.body, testDeviceID, want)
	}
}

func TestSearchTabStartsWithAHint(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)

	h.send(key("left"))

	h.wantView("Search", searchHelpText, "/ search")
	if len(h.api.searches) != 0 {
		t.Fatalf("searches = %v, want none before typing", h.api.searches)
	}
}

func TestSearchFindsEveryKind(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)
	h.send(key("left"))

	h.searchFor("beatles")

	if len(h.api.searches) != 1 {
		t.Fatalf("searches = %d, want 1", len(h.api.searches))
	}
	q := h.api.searches[0]
	if q.Get("q") != "beatles" || q.Get("type") != "track,artist,album,playlist,show,episode" || q.Get("offset") != "0" {
		t.Fatalf("search query = %v", q)
	}
	if got, want := h.searchResultNames(), []string{"Come Together", "Her Majesty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want %v", got, want)
	}
	h.wantView("Come Together - The Beatles", "4:19", "Songs", "Artists", "Albums", "Playlists", "Podcasts", "Episodes")

	h.selectSearchCategory("Playlists")
	if got, want := h.searchResultNames(), []string{"Beatles Best"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("playlists = %v, want %v", got, want)
	}
	h.wantView("Beatles Best by Ana")

	h.selectSearchCategory("Episodes")
	h.wantView(`No episodes found for "beatles".`)
}

func TestSearchKeysAreTypedWhileTyping(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)
	h.send(key("left"))

	h.send(keys("/", "k", "space", "+", "left", "right", "r")...)

	if got := h.searchTab().input.Value(); got != "k +r" {
		t.Fatalf("input = %q, want %q", got, "k +r")
	}
	if got := h.player().activeTab; got != searchTab {
		t.Fatalf("activeTab = %d, want the Search tab", got)
	}
	h.wantPauses()
	h.wantVolumes("")

	h.send(key("esc"))
	if !h.searchTab().typing {
		t.Fatal("esc left the input before any search, want it to keep typing")
	}
}

func TestSearchPlaysEachKind(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)
	h.send(key("left"))
	h.searchFor("beatles")

	h.send(key("enter"))
	h.wantBody(0, map[string]any{"uris": []any{"spotify:track:" + fmt.Sprintf("%x", "Come Together")}})
	h.wantView("⏸ Come Together — The Beatles")

	h.send(key("down"), key("enter"))
	h.wantPlays(1)
	h.wantView("Her Majesty is unavailable")

	h.selectSearchCategory("Artists")
	h.send(key("enter"))
	h.wantBody(1, map[string]any{"context_uri": "spotify:artist:ar-beatles"})

	h.selectSearchCategory("Albums")
	h.send(key("enter"))
	h.wantPlay(2, "spotify:album:a0", 0)

	h.selectSearchCategory("Podcasts")
	h.send(key("enter"))
	h.wantEpisodePlay(3, "spotify:show:s0", "How the Vote Count Works", 0)
	h.wantView("⏸ How the Vote Count Works — Morning Briefing")
}

func TestSearchLoadsMoreAsYouMoveDown(t *testing.T) {
	api := searchAPI()
	api.searchItems["track"] = manyAlbumTracksJSON(25)
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("milton")

	if got := len(h.searchResultNames()); got != 10 {
		t.Fatalf("songs = %d, want the first 10", got)
	}
	h.send(key("end"))

	if got := len(h.searchResultNames()); got != 25 {
		t.Fatalf("songs = %d, want all 25", got)
	}
	var offsets []string
	for _, q := range h.api.searches[1:] {
		if q.Get("type") != "track" {
			t.Fatalf("more search type = %q, want only track", q.Get("type"))
		}
		offsets = append(offsets, q.Get("offset"))
	}
	if want := []string{"10", "20"}; !reflect.DeepEqual(offsets, want) {
		t.Fatalf("offsets = %v, want %v", offsets, want)
	}
}

func TestSearchErrorsCanBeRetried(t *testing.T) {
	api := searchAPI()
	api.searchStatus = http.StatusInternalServerError
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("beatles")

	h.wantView("Fail to search:", "search failed", "Press r to try again.")

	api.set(func(f *fakeAPI) { f.searchStatus = 0 })
	h.send(key("r"))
	h.wantView("Come Together - The Beatles")
	if got := len(h.api.searches); got != 2 {
		t.Fatalf("searches = %d, want 2", got)
	}
}

func TestSearchAgainKeepsTheQuery(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)
	h.send(key("left"))
	h.searchFor("beatles")

	h.send(key("/"))
	if got := h.searchTab().input.Value(); got != "beatles" {
		t.Fatalf("input = %q, want the last query", got)
	}
	h.send(key("esc"))
	if h.searchTab().typing {
		t.Fatal("esc kept typing, want it back on the results")
	}
	h.wantView("Come Together - The Beatles")
}

func TestSearchFitsSmallWindows(t *testing.T) {
	for _, width := range []int{40, 60, 80} {
		h := newHarnessWith(t, searchAPI(), width, 20)
		h.send(key("left"))
		h.searchFor("beatles")
		for row := range strings.SplitSeq(h.view(), "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("row is %d wide at width %d: %q", got, width, row)
			}
		}
	}
}

func TestSearchDoesNotPlayResultsOfAnEarlierSearch(t *testing.T) {
	api := searchAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("beatles")

	api.set(func(f *fakeAPI) { f.searchStatus = http.StatusInternalServerError })
	h.send(key("r"))
	h.wantView("Fail to search:")

	h.send(key("enter"), key("down"))
	h.wantPlays(0)
	h.dontWantView("Come Together")
}

func TestSearchSortsAlbumsByYearNewestFirst(t *testing.T) {
	api := searchAPI()
	api.searchItems["album"] = []string{
		albumJSON("a-dummy", "Dummy", "album", "1994-08-22", "day", 11, "Portishead"),
		albumJSON("a-third", "Third", "album", "2008-04-28", "day", 11, "Portishead"),
		albumJSON("a-nodate", "No Date", "album", "", "", 1, "Portishead"),
		albumJSON("a-roseland", "Roseland NYC Live", "album", "2008", "year", 11, "Portishead"),
		albumJSON("a-glory", "Glory Box", "single", "1995-01", "month", 3, "Portishead"),
	}
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("portishead")

	h.selectSearchCategory("Albums")

	want := []string{"Third", "Roseland NYC Live", "Glory Box", "Dummy", "No Date"}
	if got := h.searchResultNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("albums = %v, want %v", got, want)
	}
	h.selectSearchCategory("Songs")
	if got, want := h.searchResultNames(), []string{"Come Together", "Her Majesty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want Spotify's order %v", got, want)
	}
}

func TestSearchKeepsTheSelectedAlbumWhenMoreLoad(t *testing.T) {
	api := searchAPI()
	var albums []string
	for i := range 20 {
		year := 1990 + i
		if i >= 10 {
			year = 2000 + i
		}
		albums = append(albums, albumJSON(fmt.Sprintf("al%d", i), fmt.Sprintf("Album %d", year), "album", strconv.Itoa(year), "year", 10, "Radiohead"))
	}
	api.searchItems["album"] = albums
	h := newHarnessWith(t, api, 120, 14)
	h.send(key("left"))
	h.searchFor("radiohead")
	h.selectSearchCategory("Albums")
	if got := len(h.searchResultNames()); got != 10 {
		t.Fatalf("albums = %d, want the first 10 before moving down", got)
	}

	for range 6 {
		h.send(key("down"))
	}

	names := h.searchResultNames()
	if len(names) != 20 {
		t.Fatalf("albums = %d, want 20 after moving down", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] < names[i] {
			t.Fatalf("albums = %v, want newest first", names)
		}
	}
	selected, _ := h.searchTab().selectedItem()
	if selected.name != "Album 1993" {
		t.Fatalf("selected = %q, want the album selected before the page loaded", selected.name)
	}
}

func TestSearchSortsAlbumsOfTheSameYearInSpotifysOrderAcrossPages(t *testing.T) {
	api := searchAPI()
	type album struct {
		name, date string
	}
	// Spotify's relevance order. Within a year, earlier dates come first,
	// so sorting by full date instead of year would fail.
	var spotifyOrder []album
	for i := range 20 {
		year := []int{1997, 2003, 1997, 2016, 2003}[i%5]
		month := 1 + i/2
		spotifyOrder = append(spotifyOrder, album{fmt.Sprintf("Album %02d", i), fmt.Sprintf("%d-%02d-01", year, month)})
	}
	spotifyOrder = append(spotifyOrder, album{"Undated", ""})
	api.searchItems["album"] = nil
	for _, a := range spotifyOrder {
		precision := "day"
		if a.date == "" {
			precision = ""
		}
		api.searchItems["album"] = append(api.searchItems["album"], albumJSON("id-"+a.name, a.name, "album", a.date, precision, 10, "Radiohead"))
	}
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("radiohead")

	h.selectSearchCategory("Albums")

	var want []string
	for _, year := range []string{"2016", "2003", "1997", ""} {
		for _, a := range spotifyOrder {
			if strings.HasPrefix(a.date, year) && (year != "" || a.date == "") {
				want = append(want, a.name)
			}
		}
	}
	if got := h.searchResultNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("albums =\n%v\nwant\n%v", got, want)
	}
}

func TestSearchAlbumsLoadOnePagePerKeyAtTheOldest(t *testing.T) {
	api := searchAPI()
	albums := []string{albumJSON("al-oldest", "Oldest", "album", "1950", "year", 10, "Various")}
	for i := range 300 {
		albums = append(albums, albumJSON(fmt.Sprintf("al%d", i), fmt.Sprintf("Album %d", i), "album", strconv.Itoa(2000+i%20), "year", 10, "Various"))
	}
	api.searchItems["album"] = albums
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("various")
	h.selectSearchCategory("Albums")
	searches := len(h.api.searches)

	h.send(key("end"))

	if got := len(h.api.searches) - searches; got > 1 {
		t.Fatalf("end made %d searches, want at most 1: newer albums load above the oldest, so loading must not chain", got)
	}
	if selected, _ := h.searchTab().selectedItem(); selected.name != "Oldest" {
		t.Fatalf("selected = %q, want the oldest album", selected.name)
	}
}

func TestSearchShowsEachResultOnce(t *testing.T) {
	api := searchAPI()
	api.searchItems["track"] = []string{
		albumTrackJSON("Come Together", "The Beatles", 259000, true),
		albumTrackJSON("Something", "The Beatles", 182000, true),
		albumTrackJSON("Come Together", "The Beatles", 259000, true),
	}
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("left"))
	h.searchFor("beatles")

	if got, want := h.searchResultNames(), []string{"Come Together", "Something"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want %v", got, want)
	}
}
