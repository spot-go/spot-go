package player

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func playingOnPhoneAt(volume int) *fakeAPI {
	api := playingOnPhone()
	api.volume = volume
	return api
}

func (f *fakeAPI) volumeRequests() []volumeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]volumeRequest(nil), f.volumes...)
}

func (h *harness) wantVolumes(device string, percents ...string) {
	h.t.Helper()

	var want []volumeRequest
	for _, p := range percents {
		want = append(want, volumeRequest{deviceID: device, percent: p})
	}
	if got := h.api.volumeRequests(); !slices.Equal(got, want) {
		h.t.Fatalf("volume requests = %+v, want %+v", got, want)
	}
}

func (h *harness) forgetVolume() {
	m := h.player()
	m.volume.at = time.Now().Add(-followGrace)
	h.model = m
}

func (h *harness) statusLine() string {
	h.t.Helper()

	for line := range strings.SplitSeq(h.view(), "\n") {
		if strings.Contains(line, "Volume:") {
			return line
		}
	}
	h.t.Fatalf("view does not show the volume:\n%s", h.view())
	return ""
}

func TestVolumeKeys(t *testing.T) {
	for _, tt := range []struct {
		key  string
		want string
	}{
		{"+", "55"},
		{"=", "55"},
		{"-", "45"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
			h.holdTimers = true

			h.send(key(tt.key))

			h.wantVolumes("phone", tt.want)
			h.wantView("Volume: " + tt.want + "%")
			h.wantPauses()
			h.wantPlays(0)
		})
	}
}

func TestVolumeStopsAtTheLimits(t *testing.T) {
	for _, tt := range []struct {
		name   string
		volume int
		key    string
		want   []string
		shown  string
	}{
		{"up to 100", 98, "+", []string{"100"}, "100"},
		{"at 100", 100, "+", nil, "100"},
		{"down to 0", 3, "-", []string{"0"}, "0"},
		{"at 0", 0, "-", nil, "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarnessWith(t, playingOnPhoneAt(tt.volume), 120, 40)
			h.holdTimers = true

			h.send(key(tt.key))

			h.wantVolumes("phone", tt.want...)
			h.wantView("Volume: " + tt.shown + "%")
		})
	}
}

func TestPressRightAfterAVolumeChangeUsesTheKnownVolume(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	h.send(key("+"))
	h.send(key("-"))

	h.wantPlayerReads(1)
	h.wantVolumes("phone", "55", "60", "55")
	h.wantView("Volume: 55%")
}

func TestVolumePressLaterReadsTheVolumeAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	h.forgetVolume()
	h.api.set(func(f *fakeAPI) { f.volume = 20 })
	h.send(key("+"))

	h.wantPlayerReads(2)
	h.wantVolumes("phone", "55", "25")
	h.wantView("Volume: 25%")
}

func TestVolumePressesWhileChangingAddUp(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	model, change := h.model.Update(key("+"))
	for _, k := range []string{"+", "+", "-", "+"} {
		var cmd tea.Cmd
		model, cmd = model.Update(key(k))
		if cmd != nil {
			t.Fatalf("press %q while changing the volume sent a request", k)
		}
	}
	h.model = model
	h.run(change)

	h.wantPlayerReads(1)
	h.wantVolumes("phone", "55", "65")
	h.wantView("Volume: 65%")
}

func TestPlayAfterAVolumeChangeReadsTheVolumeAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	h.send(key("enter"))
	h.send(key("+"))

	h.wantPlayerReads(2)
	h.wantVolumes("phone", "55", "60")
}

func TestVolumeHidesAfterAFewSeconds(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	h.wantView("Volume: 55%")
	if !slices.Contains(h.delays, volumeShownFor) {
		t.Fatalf("delays = %v, want the volume hidden after %v", h.delays, volumeShownFor)
	}

	h.holdTimers = false
	h.send()

	h.dontWantView("Volume:")
}

func TestOlderVolumeTimerKeepsTheNewVolume(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	first := h.timers
	h.timers = nil
	h.send(key("+"))
	h.send(first...)

	h.wantView("Volume: 60%")

	h.holdTimers = false
	h.send()

	h.dontWantView("Volume:")
}

func TestVolumeShowsNextToThePlaybackStatus(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("space"))
	h.send(key("+"))

	line := h.statusLine()
	if !strings.Contains(line, "▶ So What") || !strings.HasSuffix(strings.TrimRight(line, " │"), "Volume: 55%") {
		t.Fatalf("status line = %q, want the paused song with the volume at the end", line)
	}
}

func TestVolumeFitsANarrowWindow(t *testing.T) {
	api := playingOnPhoneAt(50)
	api.playingName = strings.Repeat("Very Long Song Name ", 10)
	h := newHarnessWith(t, api, 60, 30)
	h.holdTimers = true

	h.send(key("space"))
	h.send(key("+"))

	line := h.statusLine()
	if !strings.Contains(line, "…") || !strings.Contains(line, "Volume: 55%") {
		t.Fatalf("status line = %q, want the cut song name and the volume", line)
	}
	for row := range strings.SplitSeq(h.view(), "\n") {
		if got := ansi.StringWidth(row); got > 60 {
			t.Fatalf("row is %d columns wide, want at most 60: %q", got, row)
		}
	}
}

func TestVolumeWorksOnEveryTab(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(keys("left", "+")...)
	h.wantVolumes("phone", "55")

	for range 4 {
		h.send(key("right"))
		h.wantView("Volume: 55%")
	}
}

func TestVolumeShowsAndHidesOnEveryTab(t *testing.T) {
	for tab := playlistsTab; tab <= albumsTab; tab++ {
		t.Run(strconv.Itoa(tab), func(t *testing.T) {
			h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
			for range tab - playlistsTab {
				h.send(key("right"))
			}
			h.holdTimers = true

			h.send(key("+"))
			h.wantView("Volume: 55%")
			h.holdTimers = false
			h.send()
			h.dontWantView("Volume:")

			h.api.set(func(f *fakeAPI) { f.volumeStatus = http.StatusForbidden })
			h.holdTimers = true
			h.send(key("+"))
			h.wantView("Fail to change the volume")
			h.holdTimers = false
			h.send()
			h.dontWantView("Fail to change the volume")
		})
	}
}

func TestVolumeKeysWhileFilteringAreTyped(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)

	h.send(keys("/", "+", "=", "-")...)

	if got := h.playlists().list.FilterValue(); got != "+=-" {
		t.Fatalf("filter = %q, want %q", got, "+=-")
	}
	h.wantPlayerReads(0)
	h.wantVolumes("phone")
}

func TestVolumeWithNothingPlaying(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.holdTimers = true

	h.send(key("+"))

	h.wantVolumes("")
	h.wantView("Fail to change the volume: no active device")
}

func TestVolumeOnADeviceWithoutVolumeControl(t *testing.T) {
	api := playingOnPhoneAt(50)
	api.noVolume = true
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	h.send(key("+"))

	h.wantVolumes("")
	h.wantView("Fail to change the volume: the device has no volume control")
}

func TestVolumeFailure(t *testing.T) {
	api := playingOnPhoneAt(50)
	api.volumeStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	model, change := h.model.Update(key("+"))
	model, _ = model.Update(key("+"))
	h.model = model
	h.run(change)

	h.wantVolumes("phone", "55")
	h.wantView("Fail to change the volume: Player command failed: Cannot control device volume")

	h.api.set(func(f *fakeAPI) { f.volumeStatus = 0 })
	h.send(key("+"))

	h.wantPlayerReads(2)
	h.wantVolumes("phone", "55", "55")
	h.wantView("Volume: 55%")
	h.dontWantView("Fail to change the volume")
}

func TestVolumeErrorKeepsThePlaybackStatus(t *testing.T) {
	api := playingOnPhoneAt(50)
	api.volumeStatus = http.StatusBadGateway
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("space"))
	h.holdTimers = true

	h.send(key("+"))
	h.wantView("Fail to change the volume")

	h.holdTimers = false
	h.send()
	h.wantView("▶ So What")
	h.dontWantView("Fail to change the volume")
}

func TestFailureAfterAVolumeChangeReadsTheVolumeAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	first := h.timers
	h.timers = nil
	h.api.set(func(f *fakeAPI) { f.volumeStatus = http.StatusForbidden })
	h.send(key("+"))
	h.wantView("Fail to change the volume")
	h.dontWantView("Volume: 55%")

	h.api.set(func(f *fakeAPI) { f.volumeStatus = 0 })
	h.send(key("+"))
	h.send(first...)

	h.wantPlayerReads(2)
	h.wantVolumes("phone", "55", "60", "60")
	h.wantView("Volume: 60%")
}

func TestVolumePressesPastTheLimitKeepLaterPresses(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(90), 120, 40)
	h.holdTimers = true

	model, change := h.model.Update(key("+"))
	for _, k := range []string{"+", "+", "-"} {
		model, _ = model.Update(key(k))
	}
	h.model = model
	h.run(change)

	h.wantVolumes("phone", "95")
	h.wantView("Volume: 95%")
}

func TestPlayFinishingAroundAVolumeChangeReadsTheVolumeAgain(t *testing.T) {
	for _, playFirst := range []bool{false, true} {
		h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
		h.holdTimers = true

		model, play := h.model.Update(key("enter"))
		h.model = model
		model, change := h.model.Update(key("+"))
		h.model = model
		if playFirst {
			h.run(play)
			h.run(change)
		} else {
			h.run(change)
			h.run(play)
		}
		h.send(key("+"))

		h.wantPlayerReads(2)
	}
}

func TestPlayStartedAfterAVolumeChangeReadsTheVolumeAgain(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	model, _ := h.model.Update(key("enter"))
	h.model = model
	h.send(key("+"))

	h.wantPlayerReads(2)
}

func TestReadFailureOfTheVolume(t *testing.T) {
	api := playingOnPhoneAt(50)
	api.playerStatus = http.StatusUnauthorized
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	h.send(key("-"))

	h.wantVolumes("")
	h.wantView("Fail to change the volume: Unauthorized")
}

func TestKeyHelpShowsVolume(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.wantView("space play/pause • +/- volume")

	h.send(key("right"))
	h.wantView("space play/pause • +/- volume")
}

func TestKeyHelpLeavesOutVolumeWhenItDoesNotFit(t *testing.T) {
	h := newHarness(t, 80, 24)
	h.wantView("space play/pause")
	h.dontWantView("+/-", "…")

	h.send(key("tab"))
	h.wantView("tab playlists • enter play • r refresh • space play/pause")
	h.dontWantView("+/-")
}

type fakeVolumeFile struct {
	mu     sync.Mutex
	writes []int
	err    error
}

func (f *fakeVolumeFile) save(percent int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.writes = append(f.writes, percent)
	return f.err
}

func (f *fakeVolumeFile) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

func (f *fakeVolumeFile) wantWrites(t *testing.T, want ...int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	if !slices.Equal(f.writes, want) {
		t.Fatalf("saved volumes = %v, want %v", f.writes, want)
	}
}

func playingHereAt(volume int) *fakeAPI {
	api := playingOnPhoneAt(volume)
	api.device = testDeviceID
	return api
}

func (h *harness) saveVolumes() *fakeVolumeFile {
	file := &fakeVolumeFile{}
	m := h.player()
	m.volumeSaver = newVolumeSaver(file.save)
	h.model = m
	return file
}

func TestVolumeKeysSaveTheVolumeOfThisDevice(t *testing.T) {
	h := newHarnessWith(t, playingHereAt(50), 120, 40)
	h.holdTimers = true
	file := h.saveVolumes()

	h.send(key("+"))
	h.send(key("+"))
	h.send(key("-"))

	h.wantVolumes(testDeviceID, "55", "60", "55")
	file.wantWrites(t, 55, 60, 55)
}

func TestVolumeKeysOnAnotherDeviceDoNotSaveTheVolume(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(50), 120, 40)
	h.holdTimers = true
	file := h.saveVolumes()

	h.send(key("+"))

	h.wantVolumes("phone", "55")
	file.wantWrites(t)
}

func TestFailedVolumeChangeIsNotSaved(t *testing.T) {
	api := playingHereAt(50)
	api.volumeStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	file := h.saveVolumes()

	h.send(key("+"))

	h.wantView("Fail to change the volume")
	file.wantWrites(t)
}

func TestPolledVolumeOfThisDeviceIsSavedWhenItChanges(t *testing.T) {
	h := newHarnessWith(t, playingHereAt(30), 120, 40)
	file := h.saveVolumes()

	h.send(pollPlayback{})
	h.send(pollPlayback{})
	h.api.set(func(f *fakeAPI) { f.volume = 45 })
	h.send(pollPlayback{})

	file.wantWrites(t, 30, 45)
}

func TestPolledVolumeOfAnotherDeviceIsNotSaved(t *testing.T) {
	h := newHarnessWith(t, playingOnPhoneAt(30), 120, 40)
	file := h.saveVolumes()

	h.send(pollPlayback{})

	file.wantWrites(t)
}

func TestPolledVolumeOfADeviceWithoutVolumeControlIsNotSaved(t *testing.T) {
	api := playingHereAt(30)
	api.noVolume = true
	h := newHarnessWith(t, api, 120, 40)
	file := h.saveVolumes()

	h.send(pollPlayback{})

	file.wantWrites(t)
}

func TestPolledVolumeRightAfterAVolumeKeyIsNotSaved(t *testing.T) {
	h := newHarnessWith(t, playingHereAt(50), 120, 40)
	h.holdTimers = true
	file := h.saveVolumes()

	h.send(key("+"))
	h.api.set(func(f *fakeAPI) { f.volume = 50 })
	h.send(pollPlayback{})
	file.wantWrites(t, 55)

	h.forgetVolume()
	h.send(pollPlayback{})
	file.wantWrites(t, 55, 50)
}

func TestFailedVolumeSaveIsTriedAgainOnTheNextPoll(t *testing.T) {
	h := newHarnessWith(t, playingHereAt(30), 120, 40)
	file := h.saveVolumes()
	file.fail(errors.New("disk full"))

	h.send(pollPlayback{})
	h.send(pollPlayback{})
	file.fail(nil)
	h.send(pollPlayback{})
	h.send(pollPlayback{})

	file.wantWrites(t, 30, 30, 30)
}

func TestVolumeSaverWritesTheLatestVolumeInAnyOrder(t *testing.T) {
	file := &fakeVolumeFile{}
	s := newVolumeSaver(file.save)

	first := s.remember(55)
	second := s.remember(60)
	if again := s.remember(60); again != nil {
		t.Fatal("remembering the same volume again wants another write")
	}
	second()
	first()

	file.wantWrites(t, 60, 60)
}
