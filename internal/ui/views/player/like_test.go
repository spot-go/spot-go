package player

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const soWhat = "spotify:track:so-what"

func (f *fakeAPI) libraryCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.libraryRequests...)
}

func (f *fakeAPI) isSaved(uri string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.saved[uri]
}

func (h *harness) wantLibrary(want ...string) {
	h.t.Helper()

	if got := h.api.libraryCalls(); !slices.Equal(got, want) {
		h.t.Fatalf("library requests = %q, want %q", got, want)
	}
}

// likedSoWhat plays So What on a phone, liked when liked is true.
func likedSoWhat(liked bool) *fakeAPI {
	api := soWhatOnPixel()
	api.saved = map[string]bool{soWhat: liked}
	return api
}

func TestPlayerShowsWhetherTheSongIsLiked(t *testing.T) {
	for _, liked := range []bool{false, true} {
		h := newHarnessWith(t, likedSoWhat(liked), 120, 40)
		h.dontWantPlayerRow(0, likedMark, unlikedMark)

		h.send(pollPlayback{})

		want := unlikedMark
		if liked {
			want = likedMark
		}
		h.wantPlayerRow(0, "⏸ So What — Miles Davis "+want)
		h.wantLibrary("contains " + soWhat)

		// Polls of the same song don't ask again.
		h.send(pollPlayback{})
		h.wantLibrary("contains " + soWhat)
	}
}

func TestCompactPlayerShowsWhetherTheSongIsLiked(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(true), 120, 12)
	h.send(pollPlayback{})

	h.wantBottomLine("⏸ So What — Miles Davis " + likedMark + " ")
}

func TestLikeMarkFitsEveryWidth(t *testing.T) {
	api := likedSoWhat(true)
	api.playingName = strings.Repeat("Uma música com um nome bem comprido 👋🏽 ", 3)
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})

	for width := 50; width <= 200; width++ {
		for _, height := range []int{12, 24} {
			h.send(tea.WindowSizeMsg{Width: width, Height: height})
			v := h.view()
			if strings.Contains(v, "too small") {
				continue
			}
			if !strings.Contains(v, "⏸ Uma") {
				t.Fatalf("%dx%d: the player does not show the song:\n%s", width, height, v)
			}
			for i, line := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(line); got != width {
					t.Fatalf("%dx%d: line %d is %d columns wide, want %d:\n%s", width, height, i, got, width, v)
				}
			}
			wantFrameEdges(t, v, width)
		}
	}
}

func TestLikeKeyAddsToAndRemovesFromLikedSongs(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("l"))

	h.wantLibrary("contains "+soWhat, "PUT "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+likedMark, "Added to Liked Songs")
	if !h.api.isSaved(soWhat) {
		t.Fatal("So What is not in Liked Songs")
	}

	h.send(key("l"))

	h.wantLibrary("contains "+soWhat, "PUT "+soWhat, "DELETE "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark, "Removed from Liked Songs")
	if h.api.isSaved(soWhat) {
		t.Fatal("So What is still in Liked Songs")
	}
}

func TestLikeShowsAtOnce(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})

	var change tea.Cmd
	h.model, change = h.model.Update(key("l"))

	h.wantPlayerRow(0, "Miles Davis "+likedMark)
	h.wantLibrary("contains " + soWhat)

	h.run(change)
	h.wantLibrary("contains "+soWhat, "PUT "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+likedMark)
}

func TestLikePressedWhileChangingChangesItBack(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})

	var change tea.Cmd
	h.model, change = h.model.Update(key("l"))
	h.send(key("l"))
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark)

	h.run(change)

	h.wantLibrary("contains "+soWhat, "PUT "+soWhat, "DELETE "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark)
	if h.api.isSaved(soWhat) {
		t.Fatal("So What is in Liked Songs, want it out after the second press")
	}
}

func TestLikePressedTwiceWhileChangingKeepsTheChange(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})

	var change tea.Cmd
	h.model, change = h.model.Update(key("l"))
	h.send(keys("l", "l")...)

	h.run(change)

	h.wantLibrary("contains "+soWhat, "PUT "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+likedMark)
}

func TestLikeFailureShowsWhatWasLikedBefore(t *testing.T) {
	api := likedSoWhat(true)
	api.libraryStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("l"))

	h.wantLibrary("contains "+soWhat, "DELETE "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+likedMark, "Fail to remove from Liked Songs: Insufficient client scope")
}

func TestLikeWhileSpotGoDoesNotKnowReadsItFirst(t *testing.T) {
	api := likedSoWhat(true)
	api.containsStatus = http.StatusInternalServerError
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})
	h.dontWantPlayerRow(0, likedMark, unlikedMark)

	h.send(key("l"))
	h.wantPlayerRow(0, "Fail to like: request failed")
	h.dontWantPlayerRow(0, likedMark, unlikedMark)

	h.api.set(func(f *fakeAPI) { f.containsStatus = 0 })
	h.send(key("l"))

	h.wantLibrary("contains "+soWhat, "contains "+soWhat, "contains "+soWhat, "DELETE "+soWhat)
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark, "Removed from Liked Songs")
}

func TestLikeWithNothingPlaying(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.holdTimers = true

	h.send(key("l"))

	h.wantLibrary()
	h.wantPlayerRow(0, "Fail to like: nothing is playing")
}

func TestLikeLocalFile(t *testing.T) {
	api := soWhatOnPixel()
	api.playingItem = "spotify:local:Miles+Davis:Kind+of+Blue:So+What:562"
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})
	h.dontWantPlayerRow(0, likedMark, unlikedMark)

	h.send(key("l"))

	h.wantLibrary()
	h.wantPlayerRow(0, "Fail to like: local files can't be liked")
}

func TestLikeEpisodeAddsToYourEpisodes(t *testing.T) {
	api := soWhatOnPixel()
	api.playsElsewhere("spotify:show:s0", "episode", "spotify:episode:e1", "Inside the Final Hours")
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})
	h.wantPlayerRow(0, "Inside the Final Hours", unlikedMark)

	h.send(key("l"))

	h.wantLibrary("contains spotify:episode:e1", "PUT spotify:episode:e1")
	h.wantPlayerRow(0, likedMark, "Added to Your Episodes")
}

func TestLikeKeyWhileFilteringIsTyped(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})

	h.send(keys("/", "l")...)

	h.wantLibrary("contains " + soWhat)
	h.wantView("Filter: l")
}

func TestLikeKeyWorksOnEveryTab(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.openTab(queueTab)
	h.send(key("l"))
	h.wantPlayerRow(0, "Miles Davis "+likedMark)
	h.openTab(albumsTab)
	h.send(key("l"))
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark)
	if got := h.api.libraryCalls(); !slices.Contains(got, "PUT "+soWhat) || !slices.Contains(got, "DELETE "+soWhat) {
		t.Fatalf("library requests = %q, want a like and an unlike", got)
	}
}

func TestLikeFollowsTheSongThatPlays(t *testing.T) {
	api := likedSoWhat(false)
	api.saved["spotify:track:blue-in-green"] = true
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.wantPlayerRow(0, "So What — Miles Davis "+unlikedMark)

	h.api.set(func(f *fakeAPI) { f.playingItem, f.playingName = "spotify:track:blue-in-green", "Blue in Green" })
	h.settle()
	h.send(pollPlayback{})

	h.wantPlayerRow(0, "Blue in Green — Miles Davis "+likedMark)
	h.wantLibrary("contains "+soWhat, "contains spotify:track:blue-in-green")
}

func TestLikeAnswerAboutAnotherSongIsDropped(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})
	seq := h.player().like.seq

	h.send(likeChecked{item: "spotify:track:blue-in-green", seq: seq, liked: true})
	h.send(likeChecked{item: soWhat, seq: seq - 1, liked: true})

	h.wantPlayerRow(0, "Miles Davis "+unlikedMark)
}

func TestLikeIsCheckedAgainWhileItPlays(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})

	// The song is liked on another device.
	h.api.set(func(f *fakeAPI) { f.saved[soWhat] = true })
	m := h.player()
	m.like.checkedAt = time.Now().Add(-likeRecheckEvery + time.Minute)
	h.model = m
	h.send(progressTicked{})
	h.wantPlayerRow(0, "Miles Davis "+unlikedMark)

	m = h.player()
	m.like.checkedAt = time.Now().Add(-likeRecheckEvery)
	h.model = m
	h.send(progressTicked{})

	h.wantPlayerRow(0, "Miles Davis "+likedMark)
	h.wantLibrary("contains "+soWhat, "contains "+soWhat)
}

func TestLikeIsNotCheckedAgainWhilePaused(t *testing.T) {
	h := newHarnessWith(t, likedSoWhat(false), 120, 40)
	h.send(pollPlayback{})
	h.send(key("space"))

	m := h.player()
	m.like.checkedAt = time.Now().Add(-likeRecheckEvery)
	h.model = m
	h.send(progressTicked{})

	h.wantLibrary("contains " + soWhat)
}

func TestLikeReloadsLikedSongsWhileTheyShow(t *testing.T) {
	api := withLikedSongs()
	api.playsElsewhere("spotify:playlist:p1", "", soWhat, "So What")
	api.device, api.playingArtist = "phone", "Miles Davis"
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})
	if got := h.api.requestsFor(likedSongsKey); len(got) != 1 {
		t.Fatalf("Liked Songs requests = %q, want 1", got)
	}

	h.send(key("l"))

	if got := h.api.requestsFor(likedSongsKey); len(got) != 2 {
		t.Fatalf("Liked Songs requests = %q, want the songs loaded again", got)
	}
}

func TestLikeReloadsLikedSongsWhenTheyShowNext(t *testing.T) {
	api := withLikedSongs()
	api.playsElsewhere("spotify:playlist:p1", "", soWhat, "So What")
	api.device, api.playingArtist = "phone", "Miles Davis"
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.openTab(queueTab)

	h.send(key("l"))
	if got := h.api.requestsFor(likedSongsKey); len(got) != 1 {
		t.Fatalf("Liked Songs requests = %q, want no load while the Playlists tab doesn't show", got)
	}

	h.openTab(playlistsTab)
	if got := h.api.requestsFor(likedSongsKey); len(got) != 2 {
		t.Fatalf("Liked Songs requests = %q, want the songs loaded again", got)
	}
}

func TestLikeOfAnEpisodeKeepsLikedSongs(t *testing.T) {
	api := withLikedSongs()
	api.playsElsewhere("spotify:show:s0", "episode", "spotify:episode:e1", "Inside the Final Hours")
	api.device = "phone"
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("l"))

	h.wantLibrary("contains spotify:episode:e1", "PUT spotify:episode:e1")
	if got := h.api.requestsFor(likedSongsKey); len(got) != 1 {
		t.Fatalf("Liked Songs requests = %q, want no new load for an episode", got)
	}
}
