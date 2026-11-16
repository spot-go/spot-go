package player

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
)

const voteCountJSON = `{"type":"episode","name":"How the Vote Count Works","uri":"spotify:episode:vote","duration_ms":3134000,"show":{"name":"Morning Briefing"}}`

// queueResponse answers GET me/player/queue with what plays now and f.queue.
func (f *fakeAPI) queueResponse(r *http.Request) *http.Response {
	f.queueRequests++
	if f.queueStatus != 0 {
		return errorResponse(r, f.queueStatus, "queue failed")
	}
	playing := "null"
	if f.playing != "" && f.playingItem != "" {
		playing = fmt.Sprintf(`{"type":"track","name":%q,"uri":%q,"duration_ms":%d,"artists":[%s]}`, f.playingName, f.playingItem, f.durationMs, f.artistsJSON())
	}
	return response(r, http.StatusOK, fmt.Sprintf(`{"currently_playing":%s,"queue":[%s]}`, playing, strings.Join(f.queue, ",")))
}

// playNextInQueue plays the first item of f.queue, as a skip does.
func (f *fakeAPI) playNextInQueue() {
	var next struct {
		URI        string `json:"uri"`
		Name       string `json:"name"`
		DurationMs int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
	}
	_ = json.Unmarshal([]byte(f.queue[0]), &next)
	f.queue = f.queue[1:]
	f.playingItem, f.playingName, f.durationMs, f.progressMs, f.playingArtist = next.URI, next.Name, next.DurationMs, 0, ""
	if len(next.Artists) > 0 {
		f.playingArtist = next.Artists[0].Name
	}
}

func (f *fakeAPI) queueReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.queueRequests
}

func queueAPI() *fakeAPI {
	api := newFakeAPI()
	api.playsElsewhere("spotify:album:a0", "album", albumTrackURI("Come Together"), "Come Together")
	api.playingArtist, api.progressMs, api.durationMs = "The Beatles", 60000, 259000
	api.queue = []string{
		albumTrackJSON("Something", "The Beatles", 182000, true),
		albumTrackJSON("Maxwell's Silver Hammer", "The Beatles", 207000, true),
		albumTrackJSON("Oh! Darling", "The Beatles", 206000, true),
		voteCountJSON,
	}
	return api
}

func newQueueHarness(t *testing.T, api *fakeAPI) *harness {
	t.Helper()

	h := newHarnessWith(t, api, 120, 40)
	h.openQueueTab()
	return h
}

func (h *harness) openQueueTab() {
	h.t.Helper()

	for h.player().activeTab > queueTab {
		h.send(key("left"))
	}
}

func (h *harness) queueTab() queueModel {
	return h.player().tabs[queueTab].content.(queueModel)
}

func (h *harness) queueNames() []string {
	l := h.queueTab().list
	if l == nil {
		return nil
	}
	var names []string
	for _, li := range l.Items() {
		names = append(names, li.(songItem).Name)
	}
	return names
}

func (h *harness) selectedQueueItem() string {
	song, _ := h.queueTab().list.SelectedItem().(songItem)
	return song.Name
}

func (h *harness) wantQueue(names ...string) {
	h.t.Helper()

	if got := h.queueNames(); !reflect.DeepEqual(got, names) {
		h.t.Fatalf("queue = %q, want %q", got, names)
	}
}

func (h *harness) wantQueueReads(n int) {
	h.t.Helper()

	if got := h.api.queueReads(); got != n {
		h.t.Fatalf("queue reads = %d, want %d", got, n)
	}
}

func TestQueueIsTheFirstTab(t *testing.T) {
	h := newHarness(t, 120, 40)

	if got := h.player().tabs[0].name; got != "Queue" {
		t.Fatalf("first tab = %q, want Queue", got)
	}
	names := strings.SplitN(h.view(), "\n", 3)[1]
	if strings.Index(names, "Queue") > strings.Index(names, "Search") {
		t.Fatalf("tab names = %q, want Queue left of Search", names)
	}
	if got := h.player().activeTab; got != playlistsTab {
		t.Fatalf("activeTab = %d, want spot-go to still open on the Playlists tab", got)
	}
}

func TestQueueLoadsWhenOpened(t *testing.T) {
	api := queueAPI()
	h := newHarnessWith(t, api, 120, 40)
	h.wantQueueReads(0)

	h.openQueueTab()

	h.wantQueueReads(1)
	h.wantQueue("Something", "Maxwell's Silver Hammer", "Oh! Darling", "How the Vote Count Works")
	h.wantView(
		"Queue", "Now playing", "Come Together", "The Beatles",
		"Next up",
		"1  Something - The Beatles", "3:02",
		"4  How the Vote Count Works - Morning Briefing", "52:14",
		"enter play • r refresh • space play/pause",
	)
	h.dontWantView("tab ")
}

func TestQueuePlaysTheSelectedItemBySkippingToIt(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	h.confirmStarts()
	published := h.recordNowPlaying()

	h.send(keys("down", "down", "enter")...)

	h.wantSkips("next", "next", "next")
	h.wantPlays(0)
	wantInfo(t, published, media.Info{ID: albumTrackURI("Oh! Darling"), Title: "Oh! Darling", Artist: "The Beatles", Duration: 206 * time.Second, Playing: true})
	h.wantView("⏸ Oh! Darling — The Beatles", "Now playing")
	h.wantQueue("How the Vote Count Works")
	if got := h.queueTab().list.Index(); got != 0 {
		t.Fatalf("selection = %d, want the top of the queue", got)
	}
	// Spotify confirmed the skip, so the queue loads again.
	h.wantQueueReads(2)
}

func TestQueueSkipWaitsOnlyForTheItem(t *testing.T) {
	api := queueAPI()
	// Spotify may show another context, or none, while an item added to the
	// queue plays.
	api.onPlay = nil
	h := newQueueHarness(t, api)
	h.confirmStarts()
	published := h.recordNowPlaying()
	api.set(func(f *fakeAPI) { f.playing = "spotify:playlist:p9" })

	h.send(key("enter"))

	wantInfo(t, published, media.Info{ID: albumTrackURI("Something"), Title: "Something", Artist: "The Beatles", Duration: 182 * time.Second, Playing: true})
	h.wantPlayerReads(1)
}

func TestQueueShowsAFailedSkip(t *testing.T) {
	api := queueAPI()
	api.skipStatus = http.StatusNotFound
	h := newQueueHarness(t, api)

	h.send(keys("down", "enter")...)

	h.wantSkips("next")
	h.wantView("Fail to play: Player command failed: No active device found")
	// Some skips may have gone through, so the queue loads again.
	h.wantQueueReads(2)
	h.wantQueue("Something", "Maxwell's Silver Hammer", "Oh! Darling", "How the Vote Count Works")
}

func TestQueueIgnoresEnterWhileSkipping(t *testing.T) {
	h := newQueueHarness(t, queueAPI())

	h.send(key("down"), key("enter"), key("enter"))

	h.wantSkips("next", "next")
	if h.queueTab().skipping {
		t.Fatal("the queue is still skipping after the skip ended")
	}
	h.send(key("enter"))
	h.wantSkips("next", "next", "next")
}

func TestQueueFollowsSpotifyWhileOpen(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)

	api.set(func(f *fakeAPI) {
		f.queue = append(f.queue, albumTrackJSON("Octopus's Garden", "The Beatles", 171000, true))
	})
	h.send(pollPlayback{})
	h.wantQueueReads(2)
	h.wantView("Octopus's Garden")

	h.send(key("right"))
	api.set(func(f *fakeAPI) { f.queue = append(f.queue, albumTrackJSON("I Want You", "The Beatles", 467000, true)) })
	h.send(pollPlayback{})
	h.wantQueueReads(2)

	h.send(key("left"))
	h.wantQueueReads(3)
	h.wantView("I Want You")
}

func TestQueueLoadsAgainAfterAMediaKeySkip(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)

	h.send(media.NextTrack)

	h.wantSkips("next")
	h.wantQueueReads(2)
	h.wantQueue("Maxwell's Silver Hammer", "Oh! Darling", "How the Vote Count Works")
	h.wantView("Something")
}

func TestQueueDoesNotFollowAPollRightAfterASkip(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	h.send(key("enter"))
	h.wantQueueReads(1)

	h.send(pollPlayback{})
	h.wantQueueReads(1)

	m := h.player()
	q := m.tabs[queueTab].content.(queueModel)
	q.skippedAt = q.skippedAt.Add(-followGrace)
	m.tabs[queueTab].content = q
	h.model = m
	h.send(pollPlayback{})
	h.wantQueueReads(2)
}

func TestQueueKeepsTheSelectedItemWhenItLoadsAgain(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	h.send(keys("down", "down")...)

	api.set((*fakeAPI).playNextInQueue)
	h.send(key("r"))

	h.wantQueue("Maxwell's Silver Hammer", "Oh! Darling", "How the Vote Count Works")
	if got := h.selectedQueueItem(); got != "Oh! Darling" {
		t.Fatalf("selected = %q, want Oh! Darling", got)
	}
	h.wantView("1  Maxwell's Silver Hammer - The Beatles")
}

func TestQueueErrorsCanBeRetried(t *testing.T) {
	api := queueAPI()
	api.queueStatus = http.StatusInternalServerError
	h := newQueueHarness(t, api)

	h.wantView("Fail to load the queue:", "queue failed", "Press r to try again.")
	h.send(key("enter"), key("down"))
	h.wantSkips()

	api.set(func(f *fakeAPI) { f.queueStatus = 0 })
	h.send(key("r"))
	h.wantView("1  Something - The Beatles")

	api.set(func(f *fakeAPI) { f.queueStatus = http.StatusInternalServerError })
	h.send(key("r"))
	h.wantView("Next up (fail to load: queue failed)", "1  Something - The Beatles")
}

func TestQueueWhenNothingPlays(t *testing.T) {
	api := queueAPI()
	api.playing, api.queue = "", nil
	h := newQueueHarness(t, api)

	h.wantView(queueNothingPlaying)
	h.dontWantView("Now playing")
	h.send(key("enter"))
	h.wantSkips()

	api.set(func(f *fakeAPI) { f.playing = "spotify:album:a0" })
	h.send(key("r"))
	h.wantView("Now playing", "Come Together", queueEmptyText)
}

func TestQueueIgnoresAnEarlierLoad(t *testing.T) {
	h := newQueueHarness(t, queueAPI())
	earlier := h.queueTab().gen
	h.send(key("r"))

	h.send(queueLoaded{gen: earlier, queue: spotify.Queue{Items: []spotify.PlaylistItem{{Name: "Ghost Song", URI: "spotify:track:ghost"}}}})
	h.send(queueLoadFailed{gen: earlier, err: fmt.Errorf("late failure")})

	h.dontWantView("Ghost Song", "late failure")
	h.wantView("Something")
}

func TestQueueWrapsALongNameOnTheLeft(t *testing.T) {
	api := queueAPI()
	api.playingName = strings.Repeat("Uma música com um nome bem comprido ", 4)
	h := newQueueHarness(t, api)

	left := h.queueTab().leftView()
	lines := strings.Split(ansi.Strip(left), "\n")
	var name []string
	for _, line := range lines[3:] {
		if strings.Contains(line, "The Beatles") {
			break
		}
		name = append(name, strings.TrimRight(line, " │"))
	}
	if len(name) != queueNameLines || !strings.HasSuffix(name[len(name)-1], "…") {
		t.Fatalf("name lines = %q, want %d lines ending in …", name, queueNameLines)
	}
}

func TestQueueFitsSmallWindows(t *testing.T) {
	api := queueAPI()
	api.playingName = strings.Repeat("Uma música com um nome bem comprido 👋🏽 ", 3)
	h := newQueueHarness(t, api)

	for width := 50; width <= 120; width++ {
		for _, height := range []int{8, 14, 24} {
			h.send(tea.WindowSizeMsg{Width: width, Height: height})
			v := h.view()
			for i, row := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("%dx%d: line %d is %d columns wide:\n%s", width, height, i, got, v)
				}
			}
			if got := len(strings.Split(v, "\n")); got > height {
				t.Fatalf("%dx%d: view has %d lines:\n%s", width, height, got, v)
			}
		}
	}
}
