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

const voteCountJSON = `{"type":"episode","name":"How the Vote Count Works","uri":"spotify:episode:vote","duration_ms":3134000,"show":{"name":"Morning Briefing"},"resume_point":{"resume_position_ms":1740000}}`

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

// playNextInQueue plays the first item of f.queue, as a skip does: an
// episode from where it was stopped, and paused if Spotify was paused.
func (f *fakeAPI) playNextInQueue() {
	var next struct {
		URI        string `json:"uri"`
		Name       string `json:"name"`
		DurationMs int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		ResumePoint struct {
			PositionMs int `json:"resume_position_ms"`
		} `json:"resume_point"`
	}
	_ = json.Unmarshal([]byte(f.queue[0]), &next)
	f.queue = f.queue[1:]
	f.playingItem, f.playingName, f.durationMs, f.playingArtist = next.URI, next.Name, next.DurationMs, ""
	f.progressMs = next.ResumePoint.PositionMs
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

	h.openTab(queueTab)
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

func TestQueueIsTheFirstTabLeftOfSearch(t *testing.T) {
	h := newHarness(t, 120, 40)

	var got []string
	for _, tab := range h.player().tabs[:3] {
		got = append(got, tab.name)
	}
	if want := []string{"Queue", "Search", "Playlists"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first tabs = %q, want %q", got, want)
	}
	names := strings.SplitN(h.view(), "\n", 3)[1]
	if queue, search := strings.Index(names, "Queue"), strings.Index(names, "Search"); queue < 0 || search < queue {
		t.Fatalf("tab names = %q, want Queue, then Search", names)
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
		"enter play • r refresh",
	)
	h.wantBottomLine("space play/pause")
	h.dontWantView("tab ")
}

func TestQueuePlaysTheSelectedItemBySkippingToIt(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	published := h.recordNowPlaying()

	h.send(keys("down", "down", "enter")...)

	h.wantSkips("next", "next", "next")
	h.wantPlays(0)
	wantInfo(t, published, media.Info{ID: albumTrackURI("Oh! Darling"), Title: "Oh! Darling", Artist: "The Beatles", Duration: 206 * time.Second, Playing: true})
	h.wantView("⏸ Oh! Darling — The Beatles", "Now playing")
	// The skip reads the queue to count the items before Oh! Darling, and
	// the queue loads again after it.
	h.wantQueueReads(3)
	h.wantQueue("How the Vote Count Works")
	if got := h.queueTab().list.Index(); got != 0 {
		t.Fatalf("selection = %d, want the top of the queue", got)
	}
}

func TestQueueSkipShowsWhereSpotifyPlaysTheItem(t *testing.T) {
	api := queueAPI()
	api.paused = true
	h := newQueueHarness(t, api)
	published := h.recordNowPlaying()

	h.send(keys("end", "enter")...)

	h.wantSkips("next", "next", "next", "next")
	wantInfo(t, published, media.Info{ID: "spotify:episode:vote", Title: "How the Vote Count Works", Artist: "Morning Briefing", Duration: 3134 * time.Second, Elapsed: 29 * time.Minute})
	h.wantView("▶ How the Vote Count Works — Morning Briefing", "29:00 / 52:14")
}

func TestQueueSkipsToTheItemWhereItIsNow(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	h.send(keys("down", "down")...)

	// Come Together ended, and the queue on screen is a song behind.
	api.set((*fakeAPI).playNextInQueue)
	h.send(key("enter"))

	h.wantSkips("next", "next")
	h.wantView("⏸ Oh! Darling — The Beatles")
	h.wantQueue("How the Vote Count Works")
}

func TestQueueSkipFailsWhenTheItemLeftTheQueue(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	h.send(keys("down", "down")...)

	api.set(func(f *fakeAPI) { f.queue = f.queue[3:] })
	h.send(key("enter"))

	h.wantSkips()
	h.wantView("Fail to play: Oh! Darling is no longer in the queue")
	h.wantQueue("How the Vote Count Works")
}

func TestQueueCountsTheEntriesItCantShow(t *testing.T) {
	api := queueAPI()
	api.queue = []string{api.queue[0], "null", api.queue[2]}
	h := newQueueHarness(t, api)
	h.wantView("1  Something - The Beatles", "3  Oh! Darling - The Beatles")

	h.send(keys("down", "enter")...)

	h.wantSkips("next", "next", "next")
	h.wantView("⏸ Oh! Darling — The Beatles")
}

func TestQueueShowsAFailedSkip(t *testing.T) {
	api := queueAPI()
	api.skipStatus = http.StatusNotFound
	h := newQueueHarness(t, api)

	h.send(keys("down", "enter")...)

	h.wantSkips("next")
	h.wantView("Fail to play: Player command failed: No active device found")
	// Some skips may have gone through, so the queue loads again.
	h.wantQueueReads(3)
	h.wantQueue("Something", "Maxwell's Silver Hammer", "Oh! Darling", "How the Vote Count Works")
}

func TestQueueIgnoresEnterWhileSkipping(t *testing.T) {
	h := newQueueHarness(t, queueAPI())

	h.send(key("down"), key("enter"), key("enter"))

	h.wantSkips("next", "next")
	if h.queueTab().skipping {
		t.Fatal("the queue is still skipping after the skip ended")
	}
	h.send(key("home"), key("enter"))
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

func TestQueueDropsLoadsFromBeforeASkip(t *testing.T) {
	api := queueAPI()
	h := newQueueHarness(t, api)
	before := h.queueTab().gen
	h.send(key("down"))

	// enter starts the skip; hold it to send what arrives while it runs.
	model, skip := h.model.Update(key("enter"))
	h.model = model
	h.send(queueLoaded{gen: before, queue: spotify.Queue{Items: []spotify.PlaylistItem{{Name: "Ghost Song", URI: "spotify:track:ghost"}}}})
	h.dontWantView("Ghost Song")
	h.send(pollPlayback{}, key("r"))
	h.wantQueueReads(1)

	h.run(skip)
	h.wantSkips("next", "next")
	h.wantQueue("Oh! Darling", "How the Vote Count Works")
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
	model, load := h.model.Update(key("r"))
	h.model = model
	h.wantView(queueLoadingText)
	h.dontWantView("Fail to load the queue")
	h.run(load)
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

	// The player clips what a tab draws, so check the tab's own view.
	for width := 56; width <= 120; width++ {
		for _, height := range []int{9, 14, 24} {
			h.send(tea.WindowSizeMsg{Width: width, Height: height})
			if v := h.view(); strings.Contains(v, tooSmallWindow) {
				t.Fatalf("%dx%d: the window is too small for the Queue tab:\n%s", width, height, v)
			}
			size := tabSize(h.queueTab())
			v := h.queueTab().View().Content
			for i, row := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(row); got > size.width {
					t.Fatalf("%dx%d: line %d is %d columns wide, want at most %d:\n%s", width, height, i, got, size.width, v)
				}
			}
			if got := len(strings.Split(v, "\n")); got > size.height {
				t.Fatalf("%dx%d: the tab has %d lines, want at most %d:\n%s", width, height, got, size.height, v)
			}
		}
	}
}
