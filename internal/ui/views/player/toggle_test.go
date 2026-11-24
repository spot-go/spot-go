package player

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

func playingOnPhone() *fakeAPI {
	api := newFakeAPI()
	api.playsElsewhere("spotify:playlist:p1", "", "spotify:track:so-what", "So What")
	api.device = "phone"
	return api
}

func (f *fakeAPI) pauseRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.pauses...)
}

func (f *fakeAPI) playerReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.playerRequests
}

func (h *harness) wantPauses(devices ...string) {
	h.t.Helper()

	if got := h.api.pauseRequests(); !slices.Equal(got, devices) {
		h.t.Fatalf("pauses on %q, want %q", got, devices)
	}
}

func (h *harness) wantResume(i int, device string) {
	h.t.Helper()

	plays := h.api.playRequests()
	if len(plays) <= i {
		h.t.Fatalf("plays = %d, want at least %d", len(plays), i+1)
	}
	if plays[i].deviceID != device || len(plays[i].body) != 0 {
		h.t.Fatalf("play %d = %+v, want a resume on %q", i, plays[i], device)
	}
}

func (h *harness) wantPlayerReads(n int) {
	h.t.Helper()

	if got := h.api.playerReads(); got != n {
		h.t.Fatalf("player reads = %d, want %d", got, n)
	}
}

func (h *harness) forgetToggle() {
	m := h.player()
	m.toggle.at = time.Now().Add(-followGrace)
	h.model = m
}

func TestSpacePausesWhatPlays(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(key("space"))

	h.wantPauses("")
	h.wantPlays(0)
	h.wantView("▶ So What")
}

func TestKResumesWhatIsPaused(t *testing.T) {
	api := playingOnPhone()
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("k"))

	h.wantPauses()
	h.wantResume(0, "")
	h.wantView("⏸ So What")
}

func TestResumeWithNothingPlayingUsesSpotGo(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(keys("down", "k")...)

	h.wantResume(0, testDeviceID)
	if got := h.selected(); got != "Jazz" {
		t.Fatalf("selected = %q, want Jazz: k moved the cursor", got)
	}
	h.wantView("⏸")
}

func TestPressRightAfterAToggleUsesTheKnownState(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(key("space"))
	h.send(key("space"))

	h.wantPlayerReads(1)
	h.wantPauses("")
	h.wantResume(0, "")
	h.wantView("⏸ So What")
}

func TestPressLaterReadsTheStateAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(key("space"))
	h.forgetToggle()
	h.api.set(func(f *fakeAPI) { f.paused = false })
	h.send(key("space"))

	h.wantPlayerReads(2)
	h.wantPauses("", "")
	h.wantPlays(0)
}

func TestPlayAfterAToggleReadsTheStateAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(key("space"))
	h.send(key("enter"))
	h.send(key("space"))

	h.wantPlayerReads(2)
	h.wantPauses("", "")
	h.wantView("▶ So What")
}

func TestPressWhileTogglingIsIgnored(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	model, toggle := h.model.Update(key("space"))
	h.model = model
	model, again := h.model.Update(key("k"))
	h.model = model
	if again != nil {
		t.Fatal("a second toggle started while the first was running")
	}

	h.run(toggle)
	h.wantPauses("")
	h.wantPlays(0)

	h.send(key("k"))
	h.wantResume(0, "")
}

func TestPlayPauseKeysWhileFilteringAreTyped(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(keys("/", "k", "space")...)

	if got := h.playlists().list.FilterValue(); got != "k " {
		t.Fatalf("filter = %q, want %q", got, "k ")
	}
	h.wantPlayerReads(0)
	h.wantPauses()
	h.wantPlays(0)
}

func TestPlayPauseWorksOnEveryTab(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	h.send(keys("left", "space")...)
	h.wantPauses("")

	h.send(keys("right", "right")...)
	h.wantView("▶ So What")
	h.send(key("left"))
	h.wantView("▶ So What")
}

func TestToggleErrorsAreShown(t *testing.T) {
	api := playingOnPhone()
	api.pauseStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("space"))
	h.wantView("Fail to play/pause: Player command failed: Restriction violated")

	api.set(func(f *fakeAPI) { f.pauseStatus = 0 })
	h.send(key("space"))
	h.wantPlayerReads(2)
	h.wantView("▶ So What")
}

func TestToggleReadErrorsAreShown(t *testing.T) {
	api := playingOnPhone()
	api.playerStatus = http.StatusInternalServerError
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("space"))

	h.wantView("Fail to play/pause")
	h.wantPauses()
	h.wantPlays(0)
}

func TestPlayStartedDuringAToggleKeepsItsStatus(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	model, toggle := h.model.Update(key("space"))
	h.model = model
	h.send(key("enter"))
	h.run(toggle)

	h.wantView("⏸ Rock")
	h.dontWantView("▶")
}

func TestPausedStatusFollowsAResumeElsewhere(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)
	h.send(key("space"))
	h.wantView("▶ So What")

	h.api.set(func(f *fakeAPI) { f.paused = false })
	h.settle()
	h.send(pollPlayback{})

	h.wantView("⏸ So What")
}

func TestKeyHelpShowsPlayPause(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.wantBottomLine("space/k play/pause")

	h.send(key("right"))
	h.wantBottomLine("space/k play/pause")
}

func TestPlayFinishingAroundAToggleReadsTheStateAgain(t *testing.T) {
	for _, playFirst := range []bool{false, true} {
		h := newHarnessWith(t, playingOnPhone(), 120, 40)

		model, play := h.model.Update(key("enter"))
		h.model = model
		model, toggle := h.model.Update(key("space"))
		h.model = model
		if playFirst {
			h.run(play)
			h.run(toggle)
		} else {
			h.run(toggle)
			h.run(play)
		}
		h.send(key("space"))

		h.wantPlayerReads(2)
	}
}

func TestPollRightAfterAPauseKeepsPaused(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)
	h.send(key("space"))

	h.api.set(func(f *fakeAPI) { f.paused = false })
	h.send(pollPlayback{})

	h.wantView("▶ So What")
}

func TestResumeErrorsAreShown(t *testing.T) {
	api := playingOnPhone()
	api.paused = true
	api.playStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("k"))
	h.wantView("Fail to play/pause: Player command failed: Premium required")
	h.dontWantView("⏸ So What")

	api.set(func(f *fakeAPI) { f.playStatus = 0 })
	h.send(key("k"))
	h.wantPlayerReads(2)
	h.wantView("⏸ So What")
}

func TestEpisodeStartedDuringAToggleKeepsItsStatus(t *testing.T) {
	h := newPodcastHarness(t, playingOnPhone())
	h.send(keys("tab", "down")...)

	model, toggle := h.model.Update(key("space"))
	h.model = model
	h.send(key("enter"))
	h.run(toggle)

	h.wantView("⏸ Inside the Final Hours")
	h.dontWantView("▶")
}
