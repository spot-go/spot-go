package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/views"
	libspotify "github.com/zmb3/spotify/v2"
)

var (
	testPlaylists = []string{"Rock", "Jazz", "Samba", "Blues"}
	testCreators  = []string{"Zeca", "Ana", "Bia", "Ana"}
)

const testDeviceID = "spot-go-device"

type (
	fakeAPI struct {
		mu             sync.Mutex
		songs          map[string][]string
		songsStatus    map[string]int
		songsErr       map[string]error
		rateLimits     map[string]rateLimit
		block          map[string]chan struct{}
		totals         map[string]int
		playStatus     int
		pauseStatus    int
		songRequests   []string
		plays          []playRequest
		pauses         []string
		device         string
		skips          []string
		skipStatus     int
		nextItem       string
		nextName       string
		playingArtist  string
		recent         []recentPlay
		playing        string
		paused         bool
		playerStatus   int
		playerRequests int
		playingType    string
		playingItem    string
		playingName    string
		progressMs     int
		durationMs     int
		shows          []string
		showsStatus    int
		showRequests   int
		episodes       map[string][]string
		fresh          map[string]string
		episodeStatus  int
		episodeReads   []string
		albums         []string
		albumsStatus   int
		albumRequests  int
		albumTracks    map[string][]string
		artists        []string
		artistsStatus  int
		artistRequests int
		artistAlbums   map[string][]string
	}

	recentPlay struct {
		contextType string
		uri         string
		at          time.Time
	}

	rateLimit struct {
		offset     string
		times      int
		retryAfter string
		status     int
	}

	playRequest struct {
		deviceID string
		body     map[string]any
	}
)

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		songs: map[string][]string{
			"p0": {
				songJSON("Back in Black", "AC/DC", 255000),
				songJSON("Paranoid", "Black Sabbath", 168000),
				songJSON("Highway Star", "Deep Purple", 365000),
			},
			"p1": {songJSON("So What", "Miles Davis", 562000)},
			"p2": manySongsJSON(120),
		},
		songsStatus: map[string]int{"p3": http.StatusForbidden},
		songsErr:    map[string]error{},
		rateLimits:  map[string]rateLimit{},
		block:       map[string]chan struct{}{},
		totals:      map[string]int{},
		shows:       testShowsJSON(),
		episodes: map[string][]string{
			"s0": {
				episodeJSON(testEpisode{name: "How the Vote Count Works", date: "2024-11-07", precision: "day", durationMs: 3134000, fullyPlayed: true}),
				episodeJSON(testEpisode{name: "Inside the Final Hours", date: "2024-11-06", precision: "day", durationMs: 3600000, resumeMs: 1740000}),
				episodeJSON(testEpisode{name: "Trailer", date: "2018", precision: "year", durationMs: 125000, unplayable: true}),
			},
			"s1": manyEpisodesJSON(120),
			"s2": {},
		},
		fresh:  map[string]string{},
		albums: testSavedAlbumsJSON(),
		albumTracks: map[string][]string{
			"a0": {
				albumTrackJSON("Come Together", "The Beatles", 259000, true),
				albumTrackJSON("Something", "The Beatles", 182000, true),
				albumTrackJSON("Her Majesty", "The Beatles", 23000, false),
			},
			"a1": {albumTrackJSON("So What", "Miles Davis", 562000, true)},
			"a2": manyAlbumTracksJSON(120),
		},
		artists: testArtistsJSON(),
		artistAlbums: map[string][]string{
			"ar0": manyArtistAlbumsJSON(34),
			"ar1": {
				albumJSON("al-dummy", "Dummy", "album", "1994-08-22", "day", 11, "Portishead"),
				albumJSON("al-portishead", "Portishead", "album", "1997", "year", 11, "Portishead"),
				albumJSON("al-glory", "Glory Box", "single", "1995-01", "month", 3, "Portishead"),
			},
			"ar2": {},
		},
	}
}

var (
	testAlbums = []struct{ id, name, artist, added string }{
		{"a0", "Abbey Road", "The Beatles", "2026-10-01T10:00:00Z"},
		{"a1", "Kind of Blue", "Miles Davis", "2026-09-01T10:00:00Z"},
		{"a2", "Clube da Esquina", "Milton Nascimento", "2025-01-01T10:00:00Z"},
	}
	testArtists = []struct{ id, name string }{
		{"ar0", "Radiohead"},
		{"ar1", "Portishead"},
		{"ar2", "Björk"},
	}
)

func albumJSON(id, name, albumType, date, precision string, tracks int, artist string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:album:%s","name":%q,"album_type":%q,"release_date":%q,"release_date_precision":%q,"total_tracks":%d,"artists":[{"name":%q}]}`,
		id, id, name, albumType, date, precision, tracks, artist)
}

func savedAlbumJSON(id, name, artist, added string) string {
	return fmt.Sprintf(`{"added_at":%q,"album":%s}`, added, albumJSON(id, name, "album", "1969-09-26", "day", 17, artist))
}

func testSavedAlbumsJSON() []string {
	albums := make([]string, len(testAlbums))
	for i, a := range testAlbums {
		albums[i] = savedAlbumJSON(a.id, a.name, a.artist, a.added)
	}
	return albums
}

func albumTrackJSON(name, artist string, durationMs int, playable bool) string {
	return fmt.Sprintf(`{"type":"track","name":%q,"uri":"spotify:track:%x","duration_ms":%d,"is_playable":%t,"artists":[{"name":%q}]}`,
		name, name, durationMs, playable, artist)
}

func manyAlbumTracksJSON(n int) []string {
	tracks := make([]string, n)
	for i := range tracks {
		tracks[i] = albumTrackJSON(fmt.Sprintf("Track %d", i+1), "Milton Nascimento", 200000, true)
	}
	return tracks
}

func artistJSON(id, name string) string {
	return fmt.Sprintf(`{"id":%q,"uri":"spotify:artist:%s","name":%q}`, id, id, name)
}

func testArtistsJSON() []string {
	artists := make([]string, len(testArtists))
	for i, a := range testArtists {
		artists[i] = artistJSON(a.id, a.name)
	}
	return artists
}

func manyArtistAlbumsJSON(n int) []string {
	albums := make([]string, n)
	for i := range albums {
		albums[i] = albumJSON(fmt.Sprintf("al%d", i+1), fmt.Sprintf("Album %d", i+1), "album", fmt.Sprintf("%d", 2030-i), "year", 10, "Radiohead")
	}
	return albums
}

func songJSON(name, artist string, durationMs int) string {
	return fmt.Sprintf(`{"is_local":false,"item":{"type":"track","name":%q,"uri":"spotify:track:%x","duration_ms":%d,"artists":[{"name":%q}]}}`,
		name, name, durationMs, artist)
}

func manySongsJSON(n int) []string {
	songs := make([]string, n)
	for i := range songs {
		songs[i] = songJSON(fmt.Sprintf("Song %d", i+1), "Cartola", 180000)
	}
	return songs
}

func (f *fakeAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	switch {
	case path == "me/playlists":
		items := make([]string, len(testPlaylists))
		for i, name := range testPlaylists {
			items[i] = fmt.Sprintf(`{"id":"p%d","name":%q,"uri":"spotify:playlist:p%d","owner":{"id":"u%d","display_name":%q}}`, i, name, i, i, testCreators[i])
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"items":[%s],"total":%d,"limit":50,"offset":0}`, strings.Join(items, ","), len(items))), nil

	case path == "me/player/recently-played":
		if r.URL.Query().Get("limit") != "50" {
			return errorResponse(r, http.StatusBadRequest, "missing limit=50"), nil
		}
		items := make([]string, len(f.recent))
		for i, play := range f.recent {
			context := "null"
			if play.uri != "" {
				context = fmt.Sprintf(`{"type":%q,"uri":%q}`, play.contextType, play.uri)
			}
			items[i] = fmt.Sprintf(`{"played_at":%q,"context":%s}`, play.at.Format(time.RFC3339), context)
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"items":[%s]}`, strings.Join(items, ","))), nil

	case path == "me/player" && r.Method == http.MethodGet:
		f.playerRequests++
		if f.playerStatus != 0 {
			return errorResponse(r, f.playerStatus, http.StatusText(f.playerStatus)), nil
		}
		if f.playing == "" {
			return response(r, http.StatusNoContent, ""), nil
		}
		item, context, device := "null", fmt.Sprintf(`{"type":"playlist","uri":%q}`, f.playing), "null"
		if f.device != "" {
			device = fmt.Sprintf(`{"id":%q}`, f.device)
		}
		if f.playingItem != "" {
			item = fmt.Sprintf(`{"uri":%q,"name":%q,"duration_ms":%d,"artists":[%s]}`, f.playingItem, f.playingName, f.durationMs, f.artistsJSON())
		}
		switch f.playingType {
		case "show", "album", "artist":
			context = fmt.Sprintf(`{"type":%q,"uri":%q}`, f.playingType, f.playing)
		case "episode":
			context = "null"
			item = fmt.Sprintf(`{"type":"episode","uri":%q,"name":%q,"duration_ms":%d,"show":{"uri":%q}}`, f.playingItem, f.playingName, f.durationMs, f.playing)
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"is_playing":%t,"progress_ms":%d,"context":%s,"device":%s,"item":%s}`, !f.paused, f.progressMs, context, device, item)), nil

	case strings.HasPrefix(path, "playlists/") && strings.HasSuffix(path, "/items"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "playlists/"), "/items")
		query := r.URL.Query()
		if resp, err := f.injected(r, id, query.Get("offset")); resp != nil || err != nil {
			return resp, err
		}
		if missing := missingSongsQuery(query); missing != "" {
			return errorResponse(r, http.StatusBadRequest, "missing "+missing), nil
		}
		return f.page(r, id, f.songs[id], query), nil

	case path == "me/shows":
		f.showRequests++
		query := r.URL.Query()
		if f.showsStatus != 0 {
			return errorResponse(r, f.showsStatus, "request failed"), nil
		}
		if query.Get("limit") != "50" {
			return errorResponse(r, http.StatusBadRequest, "missing limit=50"), nil
		}
		offset, _ := strconv.Atoi(query.Get("offset"))
		page := f.shows[min(offset, len(f.shows)):min(offset+50, len(f.shows))]
		next := "null"
		if offset+50 < len(f.shows) {
			next = fmt.Sprintf(`"https://api.spotify.com/v1/me/shows?offset=%d&limit=50"`, offset+50)
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"items":[%s],"total":%d,"next":%s}`, strings.Join(page, ","), len(f.shows), next)), nil

	case strings.HasPrefix(path, "shows/") && strings.HasSuffix(path, "/episodes"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "shows/"), "/episodes")
		query := r.URL.Query()
		if resp, err := f.injected(r, id, query.Get("offset")); resp != nil || err != nil {
			return resp, err
		}
		if missing := missingEpisodesQuery(query); missing != "" {
			return errorResponse(r, http.StatusBadRequest, "missing "+missing), nil
		}
		return f.page(r, id, f.episodes[id], query), nil

	case strings.HasPrefix(path, "episodes/"):
		id := strings.TrimPrefix(path, "episodes/")
		f.episodeReads = append(f.episodeReads, id)
		if f.episodeStatus != 0 {
			return errorResponse(r, f.episodeStatus, "request failed"), nil
		}
		if r.URL.Query().Get("market") != "from_token" {
			return errorResponse(r, http.StatusBadRequest, "missing market=from_token"), nil
		}
		if fresh, ok := f.fresh[id]; ok {
			return response(r, http.StatusOK, fresh), nil
		}
		for _, episodes := range f.episodes {
			for _, e := range episodes {
				if strings.Contains(e, fmt.Sprintf(`"id":%q`, id)) {
					return response(r, http.StatusOK, e), nil
				}
			}
		}
		return errorResponse(r, http.StatusNotFound, "Non existing id"), nil

	case path == "me/albums":
		f.albumRequests++
		query := r.URL.Query()
		if f.albumsStatus != 0 {
			return errorResponse(r, f.albumsStatus, "request failed"), nil
		}
		if query.Get("limit") != "50" || query.Get("market") != "from_token" {
			return errorResponse(r, http.StatusBadRequest, "missing limit=50 or market=from_token"), nil
		}
		offset, _ := strconv.Atoi(query.Get("offset"))
		page := f.albums[min(offset, len(f.albums)):min(offset+50, len(f.albums))]
		next := "null"
		if offset+50 < len(f.albums) {
			next = fmt.Sprintf(`"https://api.spotify.com/v1/me/albums?offset=%d&limit=50"`, offset+50)
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"items":[%s],"total":%d,"next":%s}`, strings.Join(page, ","), len(f.albums), next)), nil

	case strings.HasPrefix(path, "albums/") && strings.HasSuffix(path, "/tracks"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "albums/"), "/tracks")
		query := r.URL.Query()
		if resp, err := f.injected(r, id, query.Get("offset")); resp != nil || err != nil {
			return resp, err
		}
		if query.Get("limit") != "50" || query.Get("market") != "from_token" {
			return errorResponse(r, http.StatusBadRequest, "missing limit=50 or market=from_token"), nil
		}
		return f.page(r, id, f.albumTracks[id], query), nil

	case path == "me/following":
		f.artistRequests++
		query := r.URL.Query()
		if f.artistsStatus != 0 {
			return errorResponse(r, f.artistsStatus, "request failed"), nil
		}
		if query.Get("type") != "artist" || query.Get("limit") != "50" {
			return errorResponse(r, http.StatusBadRequest, "missing type=artist or limit=50"), nil
		}
		start := 0
		if after := query.Get("after"); after != "" {
			start = -1
			for i, a := range f.artists {
				if strings.Contains(a, fmt.Sprintf(`"id":%q`, after)) {
					start = i + 1
				}
			}
			if start < 0 {
				return errorResponse(r, http.StatusBadRequest, "unknown cursor "+after), nil
			}
		}
		page := f.artists[min(start, len(f.artists)):min(start+50, len(f.artists))]
		next, cursor := "null", "null"
		if start+50 < len(f.artists) {
			var last struct{ ID string }
			_ = json.Unmarshal([]byte(page[len(page)-1]), &last)
			next = fmt.Sprintf(`"https://api.spotify.com/v1/me/following?type=artist&after=%s&limit=50"`, last.ID)
			cursor = fmt.Sprintf("%q", last.ID)
		}
		return response(r, http.StatusOK, fmt.Sprintf(`{"artists":{"items":[%s],"total":%d,"next":%s,"cursors":{"after":%s}}}`, strings.Join(page, ","), len(f.artists), next, cursor)), nil

	case strings.HasPrefix(path, "artists/") && strings.HasSuffix(path, "/albums"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "artists/"), "/albums")
		query := r.URL.Query()
		if resp, err := f.injected(r, id, query.Get("offset")); resp != nil || err != nil {
			return resp, err
		}
		if query.Get("limit") != "10" || query.Get("market") != "from_token" || query.Get("include_groups") != "album,single,compilation" {
			return errorResponse(r, http.StatusBadRequest, "missing limit=10, market=from_token or include_groups"), nil
		}
		return f.page(r, id, f.artistAlbums[id], query), nil

	case path == "me/player/play" && r.Method == http.MethodPut:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.plays = append(f.plays, playRequest{deviceID: r.URL.Query().Get("device_id"), body: body})
		if f.playStatus != 0 {
			return errorResponse(r, f.playStatus, "Player command failed: Premium required"), nil
		}
		f.paused = false
		return response(r, http.StatusNoContent, ""), nil

	case (path == "me/player/next" || path == "me/player/previous") && r.Method == http.MethodPost:
		f.skips = append(f.skips, strings.TrimPrefix(path, "me/player/"))
		if f.skipStatus != 0 {
			return errorResponse(r, f.skipStatus, "Player command failed: No active device found"), nil
		}
		if path == "me/player/next" && f.nextItem != "" {
			f.playingItem, f.playingName = f.nextItem, f.nextName
		}
		f.progressMs = 0
		return response(r, http.StatusNoContent, ""), nil

	case path == "me/player/pause" && r.Method == http.MethodPut:
		f.pauses = append(f.pauses, r.URL.Query().Get("device_id"))
		if f.pauseStatus != 0 {
			return errorResponse(r, f.pauseStatus, "Player command failed: Restriction violated"), nil
		}
		f.paused = true
		return response(r, http.StatusNoContent, ""), nil
	}

	return errorResponse(r, http.StatusNotFound, "unexpected request "+r.Method+" "+r.URL.Path), nil
}

func (f *fakeAPI) artistsJSON() string {
	if f.playingArtist == "" {
		return ""
	}
	return fmt.Sprintf(`{"name":%q}`, f.playingArtist)
}

func (f *fakeAPI) injected(r *http.Request, id, offset string) (*http.Response, error) {
	f.songRequests = append(f.songRequests, id+"@"+offset)
	if wait := f.block[id+"@"+offset]; wait != nil {
		delete(f.block, id+"@"+offset)
		f.mu.Unlock()
		<-wait
		f.mu.Lock()
	}
	if err := f.songsErr[id]; err != nil {
		return nil, err
	}
	if status := f.songsStatus[id]; status != 0 {
		return errorResponse(r, status, "request failed"), nil
	}
	if limit := f.rateLimits[id]; limit.times > 0 && limit.offset == offset {
		limit.times--
		f.rateLimits[id] = limit
		status, message := http.StatusTooManyRequests, "API rate limit exceeded"
		if limit.status != 0 {
			status, message = limit.status, http.StatusText(limit.status)
		}
		resp := errorResponse(r, status, message)
		resp.Header.Set("Retry-After", limit.retryAfter)
		return resp, nil
	}
	return nil, nil
}

func (f *fakeAPI) page(r *http.Request, id string, all []string, query url.Values) *http.Response {
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	page := all[min(offset, len(all)):min(offset+limit, len(all))]
	total := len(all)
	if t, ok := f.totals[id]; ok {
		total = t
	}
	return response(r, http.StatusOK, fmt.Sprintf(`{"items":[%s],"total":%d}`, strings.Join(page, ","), total))
}

func missingEpisodesQuery(query url.Values) string {
	if query.Get("market") != "from_token" {
		return "market=from_token"
	}
	if query.Get("limit") != "50" {
		return "limit=50"
	}
	return ""
}

func missingSongsQuery(query url.Values) string {
	if query.Get("market") != "from_token" {
		return "market=from_token"
	}
	if !strings.Contains(query.Get("additional_types"), "episode") {
		return "additional_types=episode"
	}
	if query.Get("limit") != "50" {
		return "limit=50"
	}
	for _, field := range []string{"total", "is_local", "item(", "track(", "is_playable", "duration_ms", "artists(name)", "show(name)", "uri"} {
		if !strings.Contains(query.Get("fields"), field) {
			return "fields " + field
		}
	}
	return ""
}

func (f *fakeAPI) requestsFor(playlistID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var reqs []string
	for _, r := range f.songRequests {
		if strings.HasPrefix(r, playlistID+"@") {
			reqs = append(reqs, r)
		}
	}
	return reqs
}

func (f *fakeAPI) playRequests() []playRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]playRequest(nil), f.plays...)
}

func (f *fakeAPI) set(fn func(f *fakeAPI)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	fn(f)
}

func response(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func errorResponse(r *http.Request, status int, message string) *http.Response {
	return response(r, status, fmt.Sprintf(`{"error":{"status":%d,"message":%q}}`, status, message))
}

type (
	harness struct {
		t             *testing.T
		api           *fakeAPI
		model         tea.Model
		playlistLoads int
		podcastLoads  int
		albumLoads    int
		artistLoads   int
		holdTimers    bool
		timers        []tea.Msg
		delays        []time.Duration
	}

	timerMsg struct {
		delay time.Duration
		msg   tea.Msg
	}
)

func newModel(width, height int) Model {
	return newModelWith(newFakeAPI(), width, height)
}

func newModelWith(api *fakeAPI, width, height int) Model {
	return newStoredModel(api, width, height, nil)
}

func newStoredModel(api *fakeAPI, width, height int, store *library.Store) Model {
	client := spotify.NewClient(&http.Client{Transport: api})
	size := views.WindowSize{WindowSizeMsg: tea.WindowSizeMsg{Width: width, Height: height}}
	m := NewModel(size, context.Background(), client, &libspotify.PrivateUser{}, &libspotify.PlayerDevice{ID: testDeviceID}, store, nil)
	m.skipEvery = time.Millisecond
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.after = testAfter
	playlists.pollEvery = 0
	m.tabs[playlistsTab].content = playlists
	podcasts := m.tabs[podcastsTab].content.(podcastModel)
	podcasts.after = testAfter
	m.tabs[podcastsTab].content = podcasts
	artists := m.tabs[artistsTab].content.(artistModel)
	artists.after = testAfter
	m.tabs[artistsTab].content = artists
	albums := m.tabs[albumsTab].content.(albumModel)
	albums.after = testAfter
	m.tabs[albumsTab].content = albums
	return m
}

func testAfter(d time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return timerMsg{d, msg} }
}

func newHarness(t *testing.T, width, height int) *harness {
	t.Helper()

	return newHarnessWith(t, newFakeAPI(), width, height)
}

func newHarnessWith(t *testing.T, api *fakeAPI, width, height int) *harness {
	t.Helper()

	m := newModelWith(api, width, height)
	h := &harness{t: t, api: api, model: m}
	h.run(m.Init())
	return h
}

func (h *harness) send(msgs ...tea.Msg) {
	h.t.Helper()

	queue := msgs
	for steps := 0; len(queue) > 0 || (len(h.timers) > 0 && !h.holdTimers); steps++ {
		if steps > 1000 {
			h.t.Fatal("too many messages, the model is probably in an update loop")
		}
		if len(queue) == 0 {
			queue, h.timers = h.timers, nil
		}
		msg := queue[0]
		queue = queue[1:]
		if _, ok := msg.(spinner.TickMsg); ok {
			continue
		}
		if timer, ok := msg.(timerMsg); ok {
			h.delays = append(h.delays, timer.delay)
			h.timers = append(h.timers, timer.msg)
			continue
		}
		switch msg.(type) {
		case playlistsLoaded:
			h.playlistLoads++
		case showsLoaded:
			h.podcastLoads++
		case albumsLoaded:
			h.albumLoads++
		case artistsLoaded:
			h.artistLoads++
		}

		var cmd tea.Cmd
		h.model, cmd = h.model.Update(msg)
		h.view()
		queue = append(queue, h.exec(cmd)...)
	}
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	h.send(h.exec(cmd)...)
}

func (h *harness) exec(cmd tea.Cmd) []tea.Msg {
	h.t.Helper()

	if cmd == nil {
		return nil
	}

	type result struct {
		msg     tea.Msg
		panicky any
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{panicky: r}
			}
		}()
		done <- result{msg: cmd()}
	}()

	select {
	case r := <-done:
		if r.panicky != nil {
			h.t.Fatalf("command panicked: %v", r.panicky)
		}
		if batch, ok := r.msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range batch {
				msgs = append(msgs, h.exec(c)...)
			}
			return msgs
		}
		if r.msg == nil {
			return nil
		}
		return []tea.Msg{r.msg}
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func (h *harness) view() (view string) {
	h.t.Helper()

	defer func() {
		if r := recover(); r != nil {
			h.t.Fatalf("View panicked: %v", r)
		}
	}()
	return ansi.Strip(h.model.View().Content)
}

func (h *harness) player() Model {
	return h.model.(Model)
}

func (h *harness) playlists() playlistModel {
	return h.player().tabs[playlistsTab].content.(playlistModel)
}

func (h *harness) selected() string {
	i, _ := h.playlists().list.SelectedItem().(item)
	return i.title
}

func tabSize(content tea.Model) contentDimension {
	switch c := content.(type) {
	case playlistModel:
		return contentDimension{width: c.width, height: c.height}
	case podcastModel:
		return contentDimension{width: c.width, height: c.height}
	case artistModel:
		return contentDimension{width: c.width, height: c.height}
	case albumModel:
		return contentDimension{width: c.width, height: c.height}
	case placeholderModel:
		return contentDimension{width: c.width, height: c.height}
	}
	return contentDimension{}
}

type tallModel struct{}

func (tallModel) Init() tea.Cmd                         { return nil }
func (t tallModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }
func (tallModel) View() tea.View {
	return tea.NewView(strings.Repeat(strings.Repeat("x", 300)+"\n", 100))
}

func key(k string) tea.Msg {
	switch k {
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+q":
		return tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func keys(ks ...string) []tea.Msg {
	msgs := make([]tea.Msg, len(ks))
	for i, k := range ks {
		msgs[i] = key(k)
	}
	return msgs
}

func TestPlaylistsLoad(t *testing.T) {
	h := newHarness(t, 120, 40)

	if h.playlists().state != statePlaylistLoaded {
		t.Fatalf("state = %v, want loaded", h.playlists().state)
	}
	v := h.view()
	for _, name := range testPlaylists {
		if !strings.Contains(v, name) {
			t.Fatalf("view does not show %q:\n%s", name, v)
		}
	}

	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	if resized := h.view(); resized != v {
		t.Fatalf("the first WindowSizeMsg changed the view, so the initial size was wrong:\nbefore:\n%s\nafter:\n%s", v, resized)
	}
}

func TestArrowKeysSwitchTabsWithoutPanicking(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("right", "right", "right", "right", "right", "left", "left", "left", "left", "left", "left", "right")...)

	if got := h.player().activeTab; got != playlistsTab {
		t.Fatalf("activeTab = %d, want %d", got, playlistsTab)
	}
	if h.playlistLoads != 1 {
		t.Fatalf("playlists loaded %d times, want 1", h.playlistLoads)
	}
	if h.podcastLoads != 1 {
		t.Fatalf("podcasts loaded %d times, want 1", h.podcastLoads)
	}
}

func TestTabContentFollowsActiveTab(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(key("right"))
	if v := h.view(); !strings.Contains(v, "Morning Briefing") || strings.Contains(v, "Rock") {
		t.Fatalf("Podcasts tab shows the wrong content:\n%s", v)
	}

	h.send(key("down"))
	if got := h.selectedShow(); got != "Go Weekly" {
		t.Fatalf("down on the Podcasts tab selected %q, want Go Weekly", got)
	}
	h.send(key("left"))
	if v := h.view(); !strings.Contains(v, "Rock") {
		t.Fatalf("Playlists tab does not show the playlists:\n%s", v)
	}
	if got := h.selected(); got != "Rock" {
		t.Fatalf("keys on the Podcasts tab moved the playlist cursor to %q", got)
	}

	h.send(keys("right", "right")...)
	if got := h.player().activeTab; got != artistsTab {
		t.Fatalf("activeTab = %d, want the Artists tab %d", got, artistsTab)
	}
	if v := h.view(); strings.Contains(v, "Morning Briefing") || strings.Contains(v, "Rock") {
		t.Fatalf("Artists tab shows another tab's content:\n%s", v)
	}
}

func TestPlaylistsLoadedWhileOnAnotherTab(t *testing.T) {
	m := newModel(120, 40)
	h := &harness{t: t, model: m}

	h.send(key("right"))
	h.run(m.Init())
	h.send(key("left"))

	if h.playlists().state != statePlaylistLoaded {
		t.Fatalf("state = %v, want loaded", h.playlists().state)
	}
}

func TestResizeKeepsLoadedPlaylists(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(keys("down", "down")...)

	for _, size := range [][2]int{{100, 30}, {121, 41}, {80, 24}, {120, 40}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	}
	h.send(key("right"))
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(key("left"))

	if got := h.selected(); got != "Samba" {
		t.Fatalf("selected = %q, want Samba", got)
	}
	if v := h.view(); strings.Contains(v, tooSmallWindow) || !strings.Contains(v, "Rock") {
		t.Fatalf("Playlists tab was not resized while inactive:\n%s", v)
	}
	want := h.player().contentDimension()
	for _, tab := range h.player().tabs {
		if got := tabSize(tab.content); got != want {
			t.Fatalf("%s tab size = %+v, want %+v", tab.name, got, want)
		}
	}
	if h.playlistLoads != 1 {
		t.Fatalf("playlists loaded %d times, want 1", h.playlistLoads)
	}
}

func TestArrowKeysKeepPlaylistState(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "right", "left")...)

	if got := h.selected(); got != "Jazz" {
		t.Fatalf("selected = %q, want Jazz", got)
	}
	if h.playlistLoads != 1 {
		t.Fatalf("playlists loaded %d times, want 1", h.playlistLoads)
	}
}

func TestArrowKeysMoveTheFilterCursor(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("/", "a", "m", "left", "left", "right", "x")...)

	if got := h.player().activeTab; got != playlistsTab {
		t.Fatalf("activeTab = %d, want %d", got, playlistsTab)
	}
	if got := h.playlists().list.FilterValue(); got != "axm" {
		t.Fatalf("filter = %q, want %q", got, "axm")
	}

	h.send(keys("esc", "right")...)
	if got := h.player().activeTab; got != podcastsTab {
		t.Fatalf("activeTab after leaving the filter = %d, want %d", got, podcastsTab)
	}
}

func TestFilterWithoutMatches(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("/", "z", "z", "z")...)

	if got := len(h.playlists().list.VisibleItems()); got != 0 {
		t.Fatalf("visible items = %d, want 0", got)
	}
	if got := h.playlists().list.FilterState(); got != list.Filtering {
		t.Fatalf("filter state = %v, want filtering", got)
	}
}

func TestTabWhileFilteringAppliesTheFilter(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("/", "a", "tab")...)

	if got := h.playlists().list.FilterState(); got != list.FilterApplied {
		t.Fatalf("filter state = %v, want applied", got)
	}
	if !h.playlists().leftFocus {
		t.Fatal("tab while filtering moved the focus away from the list")
	}

	h.send(key("right"))
	if got := h.player().activeTab; got != podcastsTab {
		t.Fatalf("activeTab = %d, want %d", got, podcastsTab)
	}
}

func TestFilterDoesNotHideThePlayer(t *testing.T) {
	for _, filter := range [][]string{{"/", "a"}, {"/", "a", "enter"}} {
		h := newHarness(t, 120, 40)
		h.send(keys(filter...)...)

		for width := 50; width <= 140; width++ {
			for _, height := range []int{8, 9, 14, 24, 40} {
				h.send(tea.WindowSizeMsg{Width: width, Height: height})
				v := h.view()
				fits := lipgloss.Height(v) <= height && lipgloss.Width(v) <= width
				tooSmall := strings.Contains(v, "too small")
				fitsWithoutFilter := width >= lipgloss.Width(h.player().tabRow()) && h.player().contentDimension().height >= minContentHeight
				if !fits || tooSmall == fitsWithoutFilter {
					t.Fatalf("filter %q at %dx%d: fits=%v tooSmall=%v\n%s", filter, width, height, fits, tooSmall, v)
				}
			}
		}
	}
}

func TestNoPlaylists(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(playlistsLoaded{})

	if got := len(h.playlists().list.Items()); got != 0 {
		t.Fatalf("items = %d, want 0", got)
	}
}

func TestLoadErrorIsShown(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(playlistsLoadFailed{errors.New("token expired")})
	h.send(keys("down", "/", "x")...)

	v := h.view()
	if !strings.Contains(v, "Fail to load playlists") || !strings.Contains(v, "token expired") || !strings.Contains(v, "Press r to try again") {
		t.Fatalf("view does not show the error:\n%s", v)
	}
}

func TestRetryAfterLoadError(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(playlistsLoadFailed{errors.New("service unavailable")})
	h.send(key("r"))

	if h.playlists().state != statePlaylistLoaded {
		t.Fatalf("state = %v, want loaded after retry", h.playlists().state)
	}
	if h.playlistLoads != 2 {
		t.Fatalf("playlists loaded %d times, want 2", h.playlistLoads)
	}
}

func TestOtherErrorsKeepThePlaylists(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(key("/"))
	h.send(errors.New("no clipboard utilities available"))

	if h.playlists().state != statePlaylistLoaded || h.playlists().list == nil {
		t.Fatalf("an unrelated error replaced the playlists:\n%s", h.view())
	}
}

func TestMessagesBeforeFirstResize(t *testing.T) {
	var model tea.Model = newModel(0, 0)

	model, _ = model.Update(key("down"))
	model, _ = model.Update(key("/"))
	model, _ = model.Update(spinner.New().Tick())

	if v := model.View().Content; !strings.Contains(v, tooSmallWindow) {
		t.Fatalf("view with an unknown window size = %q, want %q", v, tooSmallWindow)
	}
}

func TestViewFitsTheWindow(t *testing.T) {
	h := newHarness(t, 120, 40)

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
	if v := h.view(); strings.Contains(v, tooSmallWindow) || !strings.Contains(v, "Rock") {
		t.Fatalf("80x24 should fit the player:\n%s", v)
	}
	if h.playlistLoads != 1 {
		t.Fatalf("playlists loaded %d times, want 1", h.playlistLoads)
	}
}

func TestOversizedTabContentIsClipped(t *testing.T) {
	h := newHarness(t, 80, 24)
	m := h.player()
	m.tabs[0] = tab{name: "Search", content: tallModel{}}
	h.model = m

	h.send(keys("left")...)

	v := h.view()
	if lipgloss.Height(v) > 24 || lipgloss.Width(v) > 80 || !strings.Contains(strings.SplitN(v, "\n", 3)[1], "Search") {
		t.Fatalf("oversized content pushed the tab bar out of the window:\n%s", v)
	}
}

func TestTabMovesFocusAwayFromTheList(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("tab", "down")...)
	if got := h.selected(); got != "Rock" {
		t.Fatalf("down without focus moved the list to %q", got)
	}

	h.send(keys("tab", "down")...)
	if got := h.selected(); got != "Jazz" {
		t.Fatalf("down with focus selected %q, want Jazz", got)
	}
}

func TestViewDoesNotStartGoroutines(t *testing.T) {
	h := newHarness(t, 120, 4)
	m := h.player()

	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		_ = m.View()
	}
	if after := runtime.NumGoroutine(); after-before > 20 {
		t.Fatalf("View started goroutines: %d before, %d after", before, after)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"ctrl+c", "ctrl+q"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, 120, 40)
			h.send(key("/"))

			_, cmd := h.model.Update(key(k))
			if cmd == nil {
				t.Fatal("no command")
			}
			if cmd() != tea.Quit() {
				t.Fatal("command is not tea.Quit")
			}
		})
	}
}

func TestFrameReachesTheRightEdge(t *testing.T) {
	h := newHarness(t, 1, 24)
	minWidth := lipgloss.Width(h.player().tabRow())
	if minWidth > 80 {
		t.Fatalf("the tab bar needs %d columns, want it to fit in 80", minWidth)
	}

	for width := minWidth; width <= 200; width++ {
		h.send(tea.WindowSizeMsg{Width: width, Height: 24})
		v := h.view()
		if strings.Contains(v, tooSmallWindow) {
			t.Fatalf("%dx24 shows the too small message", width)
		}
		for i, line := range strings.Split(v, "\n") {
			if got := lipgloss.Width(line); got != width {
				t.Fatalf("%dx24: line %d is %d columns wide, want %d:\n%s", width, i, got, width, v)
			}
		}
	}
}

func TestEmojiNamesKeepTheFrameAligned(t *testing.T) {
	api := newFakeAPI()
	api.songs["p0"] = []string{songJSON("Tchau 👋🏽", "Banda 🧑🏿‍🎤", 100000)}
	h := newHarnessWith(t, api, 80, 24)

	names := []string{
		"teste 👋🏻",
		"👩🏾‍❤️‍👩🏼 casal",
		"🇧🇷 Brasil",
		"Melhores caipiras 🎻",
		"👋🏻 uma playlist com um nome bem comprido 👋🏿",
	}
	loaded := make([]libspotify.SimplePlaylist, len(names))
	for i, name := range names {
		loaded[i] = libspotify.SimplePlaylist{ID: libspotify.ID(fmt.Sprintf("p%d", i)), Name: name}
	}
	h.send(playlistsLoaded{playlists: loaded})
	h.wantView("teste 👋🏻", "Tchau 👋🏽")

	for width := 60; width <= 160; width++ {
		h.send(tea.WindowSizeMsg{Width: width, Height: 24})
		v := h.view()
		for i, line := range strings.Split(v, "\n") {
			if got := ansi.StringWidth(line); got != width {
				t.Fatalf("%dx24: line %d is %d columns wide, want %d:\n%s", width, i, got, width, v)
			}
		}
	}
}

func TestPlayerViewUsesTheAltScreen(t *testing.T) {
	h := newHarness(t, 120, 40)
	if !h.model.View().AltScreen {
		t.Fatal("player view is not on the alt screen")
	}

	h.send(tea.WindowSizeMsg{Width: 10, Height: 2})
	if !h.model.View().AltScreen {
		t.Fatal("too small window view is not on the alt screen")
	}
}
