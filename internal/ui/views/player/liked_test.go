package player

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/media"
)

const testLikedSongs = "spotify:user:chico:collection"

func likedJSON(name, artist string, durationMs int) string {
	return fmt.Sprintf(`{"added_at":"2026-11-01T10:00:00Z","track":{"type":"track","name":%q,"uri":"spotify:track:%x","duration_ms":%d,"is_local":false,"is_playable":true,"artists":[{"name":%q}]}}`,
		name, name, durationMs, artist)
}

func manyLikedJSON(n int) []string {
	songs := make([]string, n)
	for i := range songs {
		songs[i] = likedJSON(fmt.Sprintf("Liked %d", i+1), "Gal Costa", 200000)
	}
	return songs
}

func trackURI(name string) string {
	return fmt.Sprintf("spotify:track:%x", name)
}

// withLikedSongs is the fake API of an account with an ID, which has Liked
// Songs, and three songs in it.
func withLikedSongs() *fakeAPI {
	api := newFakeAPI()
	api.user = "chico"
	api.liked = []string{
		likedJSON("Baby", "Gal Costa", 211000),
		likedJSON("Aquarela", "Toquinho", 279000),
		likedJSON("Trem das Onze", "Adoniran Barbosa", 197000),
	}
	return api
}

func (h *harness) wantPlayBody(i int, want map[string]any) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	if got := plays[i]; got.deviceID != testDeviceID || !reflect.DeepEqual(got.body, want) {
		h.t.Fatalf("play %d = device %q body %v, want device %q body %v", i, got.deviceID, got.body, testDeviceID, want)
	}
}

func songURIs(names ...string) []any {
	uris := make([]any, len(names))
	for i, name := range names {
		uris[i] = trackURI(name)
	}
	return uris
}

func TestLikedSongsComesFirstInEveryOrder(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	h.wantOrder("Recents", "Liked Songs", "Rock", "Jazz", "Samba", "Blues")
	h.send(key("s"))
	h.wantOrder("Alphabetical", "Liked Songs", "Blues", "Jazz", "Rock", "Samba")
	h.send(key("s"))
	h.wantOrder("Creator", "Liked Songs", "Blues", "Jazz", "Samba", "Rock")
	h.send(key("s"))
	h.wantOrder("Custom order", "Liked Songs", "Rock", "Jazz", "Samba", "Blues")
}

func TestLikedSongsStaysFirstWhenAPlaylistIsPlayed(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	h.send(keys("down", "down", "down", "enter")...)

	h.wantOrder("Recents", "Liked Songs", "Samba", "Rock", "Jazz", "Blues")
}

func TestAccountWithoutAnIDHasNoLikedSongs(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.wantOrder("Recents", "Rock", "Jazz", "Samba", "Blues")
	if got := h.api.requestsFor(likedSongsKey); len(got) != 0 {
		t.Fatalf("Liked Songs requests = %v, want none", got)
	}
}

func TestSongsOfLikedSongsLoad(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	if got := h.selected(); got != "Liked Songs" {
		t.Fatalf("selected = %q, want Liked Songs", got)
	}
	h.wantView("Baby - Gal Costa", "3:31", "Aquarela - Toquinho", "Trem das Onze - Adoniran Barbosa", "3 songs")
	if got := h.api.requestsFor(likedSongsKey); !reflect.DeepEqual(got, []string{"liked@0"}) {
		t.Fatalf("Liked Songs requests = %v, want liked@0", got)
	}
	if got := h.api.requestsFor("p0"); len(got) != 0 {
		t.Fatalf("song requests for p0 = %v, want none before Rock is selected", got)
	}

	h.send(key("down"))
	h.wantView("Back in Black - AC/DC")
	h.send(key("up"))
	h.wantView("Baby - Gal Costa")
	if got := h.api.requestsFor(likedSongsKey); len(got) != 1 {
		t.Fatalf("Liked Songs requests = %v, want 1: the songs were loaded again", got)
	}
}

func TestAllPagesOfLikedSongsLoad(t *testing.T) {
	api := withLikedSongs()
	api.liked = manyLikedJSON(120)
	h := newHarnessWith(t, api, 120, 40)

	if got := h.api.requestsFor(likedSongsKey); !reflect.DeepEqual(got, []string{"liked@0", "liked@50", "liked@100"}) {
		t.Fatalf("Liked Songs requests = %v", got)
	}
	names := h.songNames()
	if len(names) != 120 || names[0] != "Liked 1" || names[119] != "Liked 120" {
		t.Fatalf("songs = %d, first %q, last %q", len(names), names[0], names[len(names)-1])
	}
}

func TestLikedSongsAreFiltered(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	h.send(keys("down", "/", "l", "i", "k", "e", "enter")...)

	if got := h.visibleTitles(); !reflect.DeepEqual(got, []string{"Liked Songs"}) {
		t.Fatalf("filtered = %v, want Liked Songs", got)
	}
	h.send(key("enter"))
	h.wantPlayBody(0, map[string]any{"context_uri": testLikedSongs})
}

func TestLikedSongsLoadErrorIsNotAboutPlaylists(t *testing.T) {
	api := withLikedSongs()
	api.songsStatus[likedSongsKey] = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)

	h.wantView("Fail to load songs", "Press r to try again")
	h.dontWantView("playlists you own")

	api.set(func(f *fakeAPI) { delete(f.songsStatus, likedSongsKey) })
	h.send(key("r"))
	h.wantView("Baby - Gal Costa")
}

func TestRefreshReloadsLikedSongs(t *testing.T) {
	api := withLikedSongs()
	h := newHarnessWith(t, api, 120, 40)

	api.set(func(f *fakeAPI) {
		f.liked = append([]string{likedJSON("Sozinho", "Caetano Veloso", 190000)}, f.liked...)
	})
	h.send(keys("tab", "down", "r")...)

	if got, want := h.songNames(), []string{"Sozinho", "Baby", "Aquarela", "Trem das Onze"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %q, want %q", got, want)
	}
	if got, ok := h.playlists().selectedSong(); !ok || got.Name != "Aquarela" {
		t.Fatalf("selected song = %q after the refresh, want Aquarela", got.Name)
	}
}

func TestEnterOnLikedSongsPlaysItFromTheStart(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	h.send(key("enter"))

	h.wantPlayBody(0, map[string]any{"context_uri": testLikedSongs})
	h.wantView("⏸ Liked Songs")
}

func TestEnterOnALikedSongPlaysLikedSongsFromIt(t *testing.T) {
	h := newHarnessWith(t, withLikedSongs(), 120, 40)

	h.send(keys("tab", "down", "enter")...)

	h.wantPlayBody(0, map[string]any{
		"context_uri": testLikedSongs,
		"offset":      map[string]any{"uri": trackURI("Aquarela")},
	})
	h.wantPlays(1)
	h.wantView("⏸ Aquarela")
}

func TestRefusedLikedSongPlaysTheLoadedSongsFromIt(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedOffset = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("tab", "down", "enter")...)

	h.wantPlays(2)
	h.wantPlayBody(1, map[string]any{
		"uris":   songURIs("Aquarela", "Trem das Onze"),
		"offset": map[string]any{"position": float64(0)},
	})
	h.wantView("⏸ Aquarela")
	h.dontWantView("Fail to play")
}

func TestRefusedLikedSongsPlaysTheLoadedSongsFromTheStart(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedSongs = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("enter"))

	h.wantPlays(2)
	h.wantPlayBody(1, map[string]any{
		"uris":   songURIs("Baby", "Aquarela", "Trem das Onze"),
		"offset": map[string]any{"position": float64(0)},
	})
	h.wantView("⏸ Liked Songs")
	h.dontWantView("Fail to play")
}

func TestRefusedLikedSongsSkipsUnplayableSongsAndStopsAtTheLimit(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedOffset = true
	api.liked = manyLikedJSON(maxLikedSongsPlayed + 20)
	api.liked[2] = `{"added_at":"2026-11-01T10:00:00Z","track":{"type":"track","name":"Gone","uri":"spotify:track:gone","duration_ms":1000,"is_playable":false,"artists":[]}}`
	api.liked[3] = `{"added_at":"2026-11-01T10:00:00Z","track":{"type":"track","name":"Demo","uri":"spotify:local:::Demo:100","duration_ms":100000,"is_local":true,"artists":[]}}`
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("tab", "down", "enter")...)

	plays := api.playRequests()
	if len(plays) != 2 {
		t.Fatalf("plays = %v, want 2", plays)
	}
	uris, _ := plays[1].body["uris"].([]any)
	if len(uris) != maxLikedSongsPlayed {
		t.Fatalf("songs sent = %d, want %d", len(uris), maxLikedSongsPlayed)
	}
	if uris[0] != trackURI("Liked 2") || uris[1] != trackURI("Liked 5") {
		t.Fatalf("songs sent start with %v, want Liked 2 then Liked 5, without the unplayable songs", uris[:2])
	}
}

func TestRefusedLikedSongsWithoutLoadedSongsShowsTheError(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedSongs = true
	api.liked = nil
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("enter"))

	h.wantPlays(1)
	h.wantView("Fail to play", "Invalid context uri")
}

func TestOtherLikedSongsPlayErrorsAreNotRetried(t *testing.T) {
	api := withLikedSongs()
	api.playStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("tab", "down", "enter")...)

	h.wantPlays(1)
	h.wantView("Fail to play", "Premium required")
}

func TestUnplayableLikedSongIsNotSentToSpotify(t *testing.T) {
	api := withLikedSongs()
	api.liked[0] = `{"added_at":"2026-11-01T10:00:00Z","track":{"type":"track","name":"Gone","uri":"spotify:track:gone","duration_ms":1000,"is_playable":false,"artists":[]}}`
	h := newHarnessWith(t, api, 120, 40)

	h.send(keys("tab", "enter")...)

	h.wantPlays(0)
	h.wantView("Gone is unavailable")
}

func likedBabyPlays(f *fakeAPI) {
	f.playing, f.playingType, f.playingItem, f.playingName = testLikedSongs, "collection", trackURI("Baby"), "Baby"
	f.playingArtist, f.progressMs, f.durationMs, f.paused = "Gal Costa", 0, 211000, false
}

func TestLikedSongsPlayIsConfirmed(t *testing.T) {
	api := withLikedSongs()
	api.onPlay = likedBabyPlays
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	wantInfo(t, published, media.Info{ID: trackURI("Baby"), Title: "Baby", Artist: "Gal Costa", Duration: 211 * time.Second, Playing: true})
	h.wantView("⏸ Baby")
	h.wantPlayerReads(1)
}

// TestRefusedLikedSongPlayIsConfirmed checks that spot-go waits for a song
// without a context after it plays the loaded songs: the progress Spotify
// reports only shows once the start is confirmed.
func TestRefusedLikedSongPlayIsConfirmed(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedOffset = true
	api.onPlay = func(f *fakeAPI) {
		f.playing, f.playingType, f.playingItem, f.playingName = trackURI("Aquarela"), "none", trackURI("Aquarela"), "Aquarela"
		f.playingArtist, f.progressMs, f.durationMs, f.paused = "Toquinho", 1000, 279000, false
	}
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(keys("tab", "down", "enter")...)

	h.wantPlays(2)
	wantInfo(t, published, media.Info{ID: trackURI("Aquarela"), Title: "Aquarela", Artist: "Toquinho", Duration: 279 * time.Second, Elapsed: time.Second, Playing: true})
	h.wantPlayerReads(1)
}

func TestRefusedLikedSongsPlayIsConfirmed(t *testing.T) {
	api := withLikedSongs()
	api.refuseLikedSongs = true
	api.onPlay = func(f *fakeAPI) {
		likedBabyPlays(f)
		f.playingType = "none"
	}
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(key("enter"))

	h.wantPlays(2)
	wantInfo(t, published, media.Info{ID: trackURI("Baby"), Title: "Baby", Artist: "Gal Costa", Duration: 211 * time.Second, Playing: true})
	h.wantView("⏸ Baby")
	h.wantPlayerReads(1)
}

func TestLikedSongsLoadOnceThePlaylistsTabShows(t *testing.T) {
	api := withLikedSongs()
	m := newModelWith(api, 120, 40)
	m.activeTab = searchTab
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.visible = false
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m}
	h.run(m.Init())

	if h.playlistLoads != 1 {
		t.Fatalf("playlist loads = %d, want 1: the Playlists tab loads at the start", h.playlistLoads)
	}
	if got := api.requestsFor(likedSongsKey); len(got) != 0 {
		t.Fatalf("Liked Songs requests = %v before the Playlists tab showed, want none", got)
	}

	h.openTab(playlistsTab)
	if got := api.requestsFor(likedSongsKey); !reflect.DeepEqual(got, []string{"liked@0"}) {
		t.Fatalf("Liked Songs requests = %v, want liked@0 once the tab shows", got)
	}
	h.wantView("Baby - Gal Costa")

	h.send(keys("left", "right")...)
	if got := api.requestsFor(likedSongsKey); len(got) != 1 {
		t.Fatalf("Liked Songs requests = %v, want 1: showing the tab again reloaded the songs", got)
	}
}

func TestPlaylistSongsStillLoadOnAnotherTab(t *testing.T) {
	api := newFakeAPI()
	m := newModelWith(api, 120, 40)
	m.activeTab = searchTab
	playlists := m.tabs[playlistsTab].content.(playlistModel)
	playlists.visible = false
	m.tabs[playlistsTab].content = playlists
	h := &harness{t: t, api: api, model: m}
	h.run(m.Init())

	if got := api.requestsFor("p0"); len(got) != 1 {
		t.Fatalf("song requests for p0 = %v, want 1: the first playlist loads at the start, as before", got)
	}
}

func TestLikedSongsIsNotKeptInTheHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	api := withLikedSongs()
	api.onPlay = likedBabyPlays
	h := newStoredHarness(t, api, library.NewStore(path))

	h.send(key("enter"))
	h.send(pollPlayback{})
	h.send(keys("down", "down", "enter")...)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "collection") || !strings.Contains(string(b), "spotify:playlist:p1") {
		t.Fatalf("saved history = %s", b)
	}
	h.wantOrder("Recents", "Liked Songs", "Jazz", "Rock", "Samba", "Blues")
}
