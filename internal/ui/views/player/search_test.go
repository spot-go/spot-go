package player

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/spotify"
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
		if byQuery, ok := f.searchByQuery[query.Get("q")]; ok {
			all = byQuery[kind]
		}
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

func (h *harness) openSearchTab() {
	h.t.Helper()

	for h.player().activeTab > searchTab {
		h.send(key("left"))
	}
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

// searchAgain opens the search, clears the last query with ctrl+u and
// searches for query.
func (h *harness) searchAgain(query string) {
	h.t.Helper()

	h.send(key("/"), tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
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

	h.openSearchTab()

	h.wantView("Search", searchHelpText, "/ search")
	if len(h.api.searches) != 0 {
		t.Fatalf("searches = %v, want none before typing", h.api.searches)
	}
}

func TestSearchFindsEveryKind(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)
	h.openSearchTab()

	h.searchFor("beatles")

	if len(h.api.searches) != 1 {
		t.Fatalf("searches = %d, want 1", len(h.api.searches))
	}
	q := h.api.searches[0]
	if q.Get("q") != "beatles" || q.Get("type") != "track,artist,album,playlist,show,episode" || q.Get("offset") != "0" {
		t.Fatalf("search query = %v", q)
	}
	if got := searchCategories[h.searchTab().category].name; got != "All" {
		t.Fatalf("category = %q, want All first", got)
	}
	h.wantView("All", "Songs", "Artists", "Albums", "Playlists", "Podcasts", "Episodes")

	h.selectSearchCategory("Songs")
	if got, want := h.searchResultNames(), []string{"Come Together", "Her Majesty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want %v", got, want)
	}
	h.wantView("Come Together - The Beatles", "4:19")

	h.selectSearchCategory("Playlists")
	if got, want := h.searchResultNames(), []string{"Beatles Best"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("playlists = %v, want %v", got, want)
	}
	h.wantView("Beatles Best by Ana")
	if got, want := h.searchResultLines(), []string{"Beatles Best | Ana"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("playlist rows = %v, want the owner only in the detail column: %v", got, want)
	}

	h.selectSearchCategory("Podcasts")
	if got, want := h.searchResultLines(), []string{"Morning Briefing | The Daily"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("podcast rows = %v, want the publisher only in the detail column: %v", got, want)
	}

	h.selectSearchCategory("Episodes")
	h.wantView(`No episodes found for "beatles".`)
}

func TestSearchKeysAreTypedWhileTyping(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)
	h.openSearchTab()

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
	h.openSearchTab()
	h.searchFor("beatles")
	h.selectSearchCategory("Songs")

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
	h := newHarnessWith(t, api, 120, 14)
	h.openSearchTab()
	h.searchFor("milton")
	h.selectSearchCategory("Songs")

	if got := len(h.searchResultNames()); got != 10 {
		t.Fatalf("songs = %d, want the first 10", got)
	}
	h.send(key("end"))
	if got := len(h.searchResultNames()); got != 20 {
		t.Fatalf("songs = %d, want the next page at the end", got)
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

func TestSearchFillsTheListRightAway(t *testing.T) {
	api := searchAPI()
	api.searchItems["track"] = manyAlbumTracksJSON(25)
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()
	h.searchFor("milton")
	h.selectSearchCategory("Songs")

	// Searching again from Songs keeps Songs, and fills it without a key.
	h.searchAgain("milton nascimento")
	if got := searchCategories[h.searchTab().category].name; got != "Songs" {
		t.Fatalf("category = %q, want Songs", got)
	}
	if got := len(h.searchResultNames()); got != 25 {
		t.Fatalf("songs = %d, want all 25 to fill the list without a key", got)
	}
}

func TestSearchErrorsCanBeRetried(t *testing.T) {
	api := searchAPI()
	api.searchStatus = http.StatusInternalServerError
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()
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
	h.openSearchTab()
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
		h.openSearchTab()
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
	h.openSearchTab()
	h.searchFor("beatles")

	api.set(func(f *fakeAPI) { f.searchStatus = http.StatusInternalServerError })
	h.send(key("r"))
	h.wantView("Fail to search:")

	h.send(key("enter"), key("down"))
	h.wantPlays(0)
	h.dontWantView("Come Together")
	if got := len(h.searchTab().results); got != 0 {
		t.Fatalf("results of %d kinds kept, want the earlier search's results cleared", got)
	}
}

func TestSearchIgnoresAnEarlierSearchThatLoadsLate(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()
	h.searchFor("tim bernardes")
	earlier := h.searchTab().gen
	h.searchAgain("MORNING briefing")

	h.send(searchLoaded{gen: earlier, results: spotify.SearchResults{
		Songs: []spotify.PlaylistItem{{Name: "Ghost Song", URI: "spotify:track:ghost", Playable: true}},
		Pages: map[spotify.SearchType]spotify.SearchPage{spotify.SearchSongs: {Next: 1, Total: 1, Last: true}},
	}})

	h.dontWantView("Ghost Song")
	h.wantView("Morning Briefing - The Daily")
}

func TestSearchShowsEachResultOnce(t *testing.T) {
	api := searchAPI()
	api.searchItems["track"] = []string{
		albumTrackJSON("Come Together", "The Beatles", 259000, true),
		albumTrackJSON("Something", "The Beatles", 182000, true),
		albumTrackJSON("Come Together", "The Beatles", 259000, true),
	}
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()
	h.searchFor("beatles")
	together := 0
	for _, line := range h.searchResultLines() {
		if strings.HasPrefix(line, "Come Together") {
			together++
		}
	}
	if together != 1 {
		t.Fatalf("All lists Come Together %d times, want once:\n%s", together, strings.Join(h.searchResultLines(), "\n"))
	}
	h.selectSearchCategory("Songs")

	if got, want := h.searchResultNames(), []string{"Come Together", "Something"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %v, want %v", got, want)
	}
}

func radioheadAndTimBernardesAPI() *fakeAPI {
	api := searchAPI()
	radiohead := []string{
		albumJSON("rh-okc", "OK Computer", "album", "1997-05-21", "day", 12, "Radiohead"),
		albumJSON("rh-kida", "Kid A", "album", "2000-10-02", "day", 10, "Radiohead"),
		albumJSON("rh-rainbows", "In Rainbows", "album", "2007-12-28", "day", 10, "Radiohead"),
		albumJSON("rh-moon", "A Moon Shaped Pool", "album", "2016-05-08", "day", 11, "Radiohead"),
		albumJSON("rh-bends", "The Bends", "album", "1995-03-13", "day", 12, "Radiohead"),
		albumJSON("rh-pablo", "Pablo Honey", "album", "1993-02-22", "day", 12, "Radiohead"),
		albumJSON("rh-hail", "Hail to the Thief", "album", "2003-06-09", "day", 14, "Radiohead"),
		albumJSON("rh-amnesiac", "Amnesiac", "album", "2001-06-05", "day", 11, "Radiohead"),
		albumJSON("rh-limbs", "The King of Limbs", "album", "2011-02-18", "day", 8, "Radiohead"),
		albumJSON("rh-mnesia", "KID A MNESIA", "album", "2021-11-05", "day", 34, "Radiohead"),
	}
	// Spotify keeps returning albums past the relevant ones, loosely related
	// or not at all, and many are newer than the relevant ones.
	for i := range 30 {
		radiohead = append(radiohead, albumJSON(fmt.Sprintf("other%d", i), fmt.Sprintf("Lo-Fi Beats Vol. %d", i), "album", strconv.Itoa(2022+i%4), "year", 20, "Chill Study Crew"))
	}
	api.searchByQuery = map[string]map[string][]string{
		"radiohead": {
			"track":  {albumTrackJSON("Creep", "Radiohead", 238000, true)},
			"artist": {artistJSON("ar-rh", "Radiohead")},
			"album":  radiohead,
		},
		"tim bernardes": {
			"album": {
				albumJSON("tb-recomecar", "Recomeçar", "album", "2017-07-28", "day", 11, "Tim Bernardes"),
				albumJSON("tb-mil", "Mil Coisas Invisíveis", "album", "2022-06-10", "day", 15, "Tim Bernardes"),
			},
			"artist": {artistJSON("ar-tb", "Tim Bernardes")},
			"track":  {albumTrackJSON("Recomeçar", "Tim Bernardes", 300000, true)},
		},
	}
	return api
}

func TestSearchListsAlbumsInSpotifysOrder(t *testing.T) {
	api := radioheadAndTimBernardesAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()
	h.searchFor("radiohead")

	// Chico's steps: go to Albums, search again from Albums, then go to
	// Artists and back.
	h.send(keys("tab", "down", "down", "down")...)
	h.send(key("tab"))
	h.wantSearchAlbumsInOrder(api.searchByQuery["radiohead"]["album"])

	h.searchAgain("tim bernardes")
	h.wantSearchAlbumsInOrder(api.searchByQuery["tim bernardes"]["album"])

	h.send(key("tab"), key("up"), key("down"), key("tab"))
	h.wantSearchAlbumsInOrder(api.searchByQuery["tim bernardes"]["album"])
}

// wantSearchAlbumsInOrder checks that the Albums results start with the
// albums of spotify, in its order, with the first one selected.
func (h *harness) wantSearchAlbumsInOrder(spotify []string) {
	h.t.Helper()

	if got := searchCategories[h.searchTab().category].name; got != "Albums" {
		h.t.Fatalf("category = %q, want Albums", got)
	}
	r := h.searchTab().selectedResults()
	items := r.list.Items()
	if len(items) == 0 || len(items) > len(spotify) {
		h.t.Fatalf("albums = %d, want between 1 and %d:\n%s", len(items), len(spotify), h.view())
	}
	for i, li := range items {
		if want := fmt.Sprintf(`"uri":"spotify:album:%s"`, li.(searchItem).id); !strings.Contains(spotify[i], want) {
			h.t.Fatalf("album %d = %q, want Spotify's album %d: %s\n%s", i, li.(searchItem).name, i, spotify[i], h.view())
		}
	}
	if r.list.Index() != 0 {
		h.t.Fatalf("selected album = %d, want the first", r.list.Index())
	}
}

func playlistJSON(id, name, owner string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:playlist:%s","name":%q,"owner":{"display_name":%q}}`, id, id, name, owner)
}

func searchShowJSON(id, name, publisher string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:show:%s","name":%q,"publisher":%q}`, id, id, name, publisher)
}

// timBernardesAPI returns results like the ones the Spotify app shows for
// "tim bernardes".
func timBernardesAPI() *fakeAPI {
	api := searchAPI()
	api.searchByQuery = map[string]map[string][]string{
		"tim bernardes": {
			"artist": {artistJSON("ar-tb", "Tim Bernardes"), artistJSON("ar-terno", "O Terno")},
			"album": {
				albumJSON("al-mil", "Mil Coisas Invisíveis", "album", "2022-06-10", "day", 15, "Tim Bernardes"),
				albumJSON("al-recomecar", "Recomeçar", "album", "2017-07-28", "day", 11, "Tim Bernardes"),
				albumJSON("al-atras", "<atrás/além>", "album", "2019-04-02", "day", 10, "O Terno"),
			},
			"track": {
				albumTrackJSON("eu vou", "O Terno", 240000, true),
				albumTrackJSON("A Balada de Tim Bernardes", "Tim Bernardes", 250000, true),
				albumTrackJSON("BB (Garupa de Moto Amarela)", "Tim Bernardes", 260000, true),
			},
			"playlist": {
				playlistJSON("pl-this", "This Is Tim Bernardes", "Spotify"),
				playlistJSON("pl-cala", "cala a boca tim bernardes", "Thales Moutinho"),
			},
			"show":    {searchShowJSON("sh-musica", "Música Brasileira Hoje", "Rádio")},
			"episode": {episodeJSON(testEpisode{name: "Entrevista com Tim Bernardes", date: "2024-03-01", precision: "day", durationMs: 1800000})},
		},
		"MORNING briefing": {
			"artist":  {artistJSON("ar-morning", "Morning Glory")},
			"show":    {searchShowJSON("s0", "Morning Briefing", "The Daily")},
			"episode": {episodeJSON(testEpisode{name: "How the Vote Count Works", date: "2024-11-07", precision: "day", durationMs: 3134000})},
			"track":   {albumTrackJSON("Morning", "Beck", 300000, true)},
		},
	}
	return api
}

func (h *harness) searchResultLines() []string {
	var lines []string
	for _, li := range h.searchTab().selectedResults().list.Items() {
		item := li.(searchItem)
		lines = append(lines, item.label()+" | "+item.detail)
	}
	return lines
}

func TestSearchAllMixesTheBestOfEachKind(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()

	h.searchFor("tim bernardes")

	want := []string{
		"Tim Bernardes | Artist",
		"Mil Coisas Invisíveis - Tim Bernardes | Album",
		"eu vou - O Terno | Song",
		"This Is Tim Bernardes - Spotify | Playlist",
		"Recomeçar - Tim Bernardes | Album",
		"A Balada de Tim Bernardes - Tim Bernardes | Song",
		"cala a boca tim bernardes - Thales Moutinho | Playlist",
		"<atrás/além> - O Terno | Album",
		"BB (Garupa de Moto Amarela) - Tim Bernardes | Song",
		"Música Brasileira Hoje - Rádio | Podcast",
		"Entrevista com Tim Bernardes | Episode",
	}
	if got := h.searchResultLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("all =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if top := h.searchTab().selectedResults().list.Items()[0].(searchItem); !top.top {
		t.Fatal("the artist is not marked as the top result")
	}
	for _, row := range []string{`> Tim Bernardes +Artist +│`, `This Is Tim Bernardes - Spotify +Playlist +│`, `eu vou - O Terno +Song +│`, `Recomeçar - Tim Bernardes +Album +│`, `Entrevista com Tim Bernardes +Episode +│`} {
		if !regexp.MustCompile(row).MatchString(h.view()) {
			t.Fatalf("view has no row matching %q:\n%s", row, h.view())
		}
	}
	h.send(key("tab"))
	h.wantView(`Results for "tim bernardes"`)
	if !regexp.MustCompile(`All +11 │`).MatchString(h.view()) {
		t.Fatalf("view does not count 11 results:\n%s", h.view())
	}
}

func TestSearchAllPutsAPodcastNamedLikeTheSearchFirst(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()

	h.searchFor("MORNING briefing")

	want := []string{
		"Morning Briefing - The Daily | Podcast",
		"How the Vote Count Works | Episode",
		"Morning - Beck | Song",
	}
	if got := h.searchResultLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("all =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSearchAllPlaysEachKind(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()
	h.searchFor("tim bernardes")

	h.send(key("enter"))
	h.wantBody(0, map[string]any{"context_uri": "spotify:artist:ar-tb"})

	h.send(key("down"), key("enter"))
	h.wantPlay(1, "spotify:album:al-mil", 0)

	h.send(key("down"), key("enter"))
	h.wantBody(2, map[string]any{"uris": []any{"spotify:track:" + fmt.Sprintf("%x", "eu vou")}})
	h.wantView("⏸ eu vou — O Terno")

	h.send(key("down"), key("enter"))
	h.wantPlay(3, "spotify:playlist:pl-this", 0)
}

func TestSearchAllDoesNotLoadMore(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 14)
	h.openSearchTab()
	h.searchFor("tim bernardes")

	h.send(key("end"), key("down"), key("end"))

	if got := len(h.api.searches); got != 1 {
		t.Fatalf("searches = %d, want only the first one: All mixes the first page of each kind", got)
	}
}

func TestSearchAllWithoutResults(t *testing.T) {
	api := timBernardesAPI()
	api.searchByQuery["zzzz"] = map[string][]string{}
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()

	h.searchFor("zzzz")

	h.wantView(`No results found for "zzzz".`)
	h.send(key("enter"))
	h.wantPlays(0)
}

func TestSearchAllWithoutAnExactMatch(t *testing.T) {
	h := newHarnessWith(t, searchAPI(), 120, 40)
	h.openSearchTab()

	h.searchFor("beatles")

	want := []string{
		"The Beatles | Artist",
		"Abbey Road - The Beatles | Album",
		"Come Together - The Beatles | Song",
		"Beatles Best - Ana | Playlist",
		"Her Majesty - The Beatles | Song",
		"Morning Briefing - The Daily | Podcast",
	}
	if got := h.searchResultLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("all =\n%s\nwant the first artist on top\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSearchAllTopResultIsTheExactNameAnywhereInItsKind(t *testing.T) {
	api := timBernardesAPI()
	api.searchByQuery["o terno"] = api.searchByQuery["tim bernardes"]
	api.searchByQuery["musica  brasileira HOJE"] = api.searchByQuery["tim bernardes"]
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()

	h.searchFor("o terno")
	if got := h.searchResultLines()[0]; got != "O Terno | Artist" {
		t.Fatalf("top = %q, want the second artist, named as the search", got)
	}

	// Accents, case and spaces aside, the podcast is named as the search,
	// so it comes first, before the first artist, with the episodes.
	h.searchAgain("musica  brasileira HOJE")
	lines := h.searchResultLines()
	if want := []string{"Música Brasileira Hoje - Rádio | Podcast", "Entrevista com Tim Bernardes | Episode", "Mil Coisas Invisíveis - Tim Bernardes | Album"}; !reflect.DeepEqual(lines[:3], want) {
		t.Fatalf("all starts with\n%s\nwant\n%s", strings.Join(lines[:3], "\n"), strings.Join(want, "\n"))
	}
}

func TestSearchAllWithPodcastsAndNoArtists(t *testing.T) {
	api := timBernardesAPI()
	api.searchByQuery["daily news"] = map[string][]string{
		"show":    {searchShowJSON("s0", "Morning Briefing", "The Daily")},
		"episode": {episodeJSON(testEpisode{name: "Inside the Final Hours", date: "2024-11-06", precision: "day", durationMs: 3600000, resumeMs: 1740000})},
		"track":   {albumTrackJSON("News", "Band", 200000, true)},
	}
	h := newHarnessWith(t, api, 120, 40)
	h.openSearchTab()
	h.searchFor("daily news")

	want := []string{
		"Morning Briefing - The Daily | Podcast",
		"Inside the Final Hours | Episode",
		"News - Band | Song",
	}
	if got := h.searchResultLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("all =\n%s\nwant the first podcast on top\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The podcast and the episode play as they do in their own kinds: the
	// newest episode of the podcast, and the episode from where you stopped.
	h.send(key("enter"))
	h.wantEpisodePlay(0, "spotify:show:s0", "How the Vote Count Works", 0)
	h.send(key("down"), key("enter"))
	h.wantBody(1, map[string]any{"uris": []any{episodeURI("Inside the Final Hours")}, "position_ms": float64(1740000)})
}

func TestSearchArrowsBeforeTheFirstSearchKeepAll(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()

	h.send(keys("down", "down", "up", "down")...)
	h.searchFor("tim bernardes")

	if got := searchCategories[h.searchTab().category].name; got != "All" {
		t.Fatalf("category = %q, want All", got)
	}
}

func TestSearchAgainLeavesAKindWithoutResults(t *testing.T) {
	h := newHarnessWith(t, timBernardesAPI(), 120, 40)
	h.openSearchTab()
	h.searchFor("tim bernardes")
	h.selectSearchCategory("Playlists")

	h.searchAgain("MORNING briefing")

	if got := searchCategories[h.searchTab().category].name; got != "All" {
		t.Fatalf("category = %q, want All when Playlists has nothing", got)
	}
	h.dontWantView("No playlists found")
}

func TestSearchNamesFoldOnlyAccents(t *testing.T) {
	tests := []struct {
		a, b string
		same bool
	}{
		{"Beyoncé", "beyonce", true},
		{"Tim  Bernardes ", " tim bernardes", true},
		{"Música Brasileira Hoje", "MUSICA brasileira hoje", true},
		{"Björk", "bjork", true},
		{"バンド", "ハンド", false},
		{"ガラス", "カラス", false},
		{"कुल", "कल", false},
		{"Tim Bernardes", "O Terno", false},
	}
	for _, tt := range tests {
		if got := sameName(tt.a, tt.b); got != tt.same {
			t.Errorf("sameName(%q, %q) = %t, want %t", tt.a, tt.b, got, tt.same)
		}
	}
}
