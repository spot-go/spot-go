package player

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

// playerRows are the two rows of the full player, without the frame around
// them.
func (h *harness) playerRows() []string {
	h.t.Helper()

	lines := strings.Split(h.view(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "├") && strings.HasSuffix(lines[i], "┤") {
			if i+2 >= len(lines) {
				break
			}
			rows := []string{lines[i+1], lines[i+2]}
			for j, row := range rows {
				rows[j] = strings.TrimSuffix(strings.TrimPrefix(row, "│ "), " │")
			}
			return rows
		}
	}
	h.t.Fatalf("view does not show the full player:\n%s", h.view())
	return nil
}

func (h *harness) wantPlayerRow(row int, texts ...string) {
	h.t.Helper()

	got := h.playerRows()[row]
	for _, text := range texts {
		if !strings.Contains(got, text) {
			h.t.Fatalf("player row %d = %q, want %q in it", row, got, text)
		}
	}
}

func (h *harness) dontWantPlayerRow(row int, texts ...string) {
	h.t.Helper()

	got := h.playerRows()[row]
	for _, text := range texts {
		if strings.Contains(got, text) {
			h.t.Fatalf("player row %d = %q, want no %q in it", row, got, text)
		}
	}
}

// bottomLine is the last line of the frame: the key hints of the full
// player, or what plays in the compact one.
func (h *harness) bottomLine() string {
	h.t.Helper()

	lines := strings.Split(h.view(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "└") {
			return lines[i]
		}
	}
	h.t.Fatalf("view has no bottom line:\n%s", h.view())
	return ""
}

func (h *harness) wantBottomLine(texts ...string) {
	h.t.Helper()

	got := h.bottomLine()
	for _, text := range texts {
		if !strings.Contains(got, text) {
			h.t.Fatalf("bottom line = %q, want %q in it", got, text)
		}
	}
}

// wantVolumeMeter checks that the player ends its second row with the
// volume.
func (h *harness) wantVolumeMeter(percent int) {
	h.t.Helper()

	row := strings.TrimRight(h.playerRows()[1], " ")
	if want := fmt.Sprintf(" %d%%", percent); !strings.HasSuffix(row, want) {
		h.t.Fatalf("player row 1 = %q, want it to end with the volume%s", row, want)
	}
}

func (f *fakeAPI) modeRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.modes...)
}

func (f *fakeAPI) seekRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.seeks...)
}

func (h *harness) wantModes(want ...string) {
	h.t.Helper()

	if got := h.api.modeRequests(); !slices.Equal(got, want) {
		h.t.Fatalf("mode requests = %q, want %q", got, want)
	}
}

func (h *harness) wantSeeks(want ...string) {
	h.t.Helper()

	if got := h.api.seekRequests(); !slices.Equal(got, want) {
		h.t.Fatalf("seek requests = %q, want %q", got, want)
	}
}

// forgetModes makes spot-go forget it changed shuffle and repeat a moment
// ago.
func (h *harness) forgetModes() {
	m := h.player()
	for i := range m.modes {
		m.modes[i].at = time.Now().Add(-followGrace)
	}
	h.model = m
}

func soWhatOnPixel() *fakeAPI {
	api := soWhatOnPhone()
	api.deviceName = "Pixel 7"
	api.volume = 60
	return api
}

func TestPlayerIsAtTheBottomOfEveryTab(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.send(pollPlayback{})

	for _, tab := range []int{queueTab, searchTab, playlistsTab, podcastsTab, artistsTab, albumsTab} {
		h.openTab(tab)

		lines := strings.Split(h.view(), "\n")
		if len(lines) != 40 {
			t.Fatalf("%s tab: view has %d lines, want 40", h.player().tabs[tab].name, len(lines))
		}
		player := lines[35:]
		if !strings.HasPrefix(player[0], "├─") || !strings.HasSuffix(player[0], "─┤") ||
			!strings.Contains(player[1], "⏸ So What — Miles Davis") || !strings.Contains(player[1], "on Pixel 7") ||
			!strings.Contains(player[2], "1:00 / 9:22") || !strings.HasSuffix(strings.TrimRight(player[2], " │"), "60%") ||
			!strings.HasPrefix(player[3], "└─ space play/pause") || !strings.HasSuffix(player[3], "─┘") ||
			strings.TrimSpace(player[4]) != "" {
			t.Fatalf("%s tab: bottom of the window = %q, want the player", h.player().tabs[tab].name, player)
		}
	}
}

func TestPlayerShowsTheSongAboveItsProgress(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.send(pollPlayback{})

	rows := h.playerRows()
	icon, bar := strings.Index(rows[0], "⏸"), strings.Index(rows[1], wideBarFilled)
	if icon < 0 || bar < 0 || ansi.StringWidth(rows[0][:icon]) != equalizerWidth+1 || ansi.StringWidth(rows[1][:bar]) != equalizerWidth+1 {
		t.Fatalf("player rows = %q, want the progress bar to start under the play/pause icon, after the equalizer", rows)
	}
}

func TestPlayerShowsAPauseWithAQuietEqualizer(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.send(pollPlayback{})
	h.dontWantPlayerRow(0, quietEqualizer)

	h.send(key("space"))

	h.wantPlayerRow(0, quietEqualizer+" ▶ So What — Miles Davis")
	h.wantPlayerRow(1, "1:00 / 9:22")
}

func TestCompactPlayerOnShortWindows(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.api.set(func(f *fakeAPI) { f.shuffle, f.repeat = true, "context" })
	h.send(pollPlayback{})

	for height := 9; height <= 17; height++ {
		h.send(tea.WindowSizeMsg{Width: 120, Height: height})
		v := h.view()
		lines := strings.Split(v, "\n")
		full := height >= 16
		if full != strings.HasPrefix(lines[height-5], "├") {
			t.Fatalf("120x%d: full player = %t, want %t:\n%s", height, !full, full, v)
		}
		if full {
			continue
		}
		line := lines[height-2]
		if want := "└─ 🎧 ⏸ So What — Miles Davis " + bar(1) + " 1:00 / 9:22 🎧 ─"; !strings.HasPrefix(line, want) {
			t.Fatalf("120x%d: bottom line = %q, want it to start with %q", height, line, want)
		}
		if !strings.HasSuffix(line, " ⇄  ↻  🔉 60% ─┘") {
			t.Fatalf("120x%d: bottom line = %q, want shuffle, repeat and the volume at its end", height, line)
		}
	}
}

func TestPlayerFitsEveryWidth(t *testing.T) {
	api := soWhatOnPixel()
	api.playingName = strings.Repeat("Uma música com um nome bem comprido 👋🏽 ", 3)
	api.playingArtist = "Banda 🧑🏿‍🎤 com nome longo"
	api.deviceName = "Caixa de som 🔊 da sala de estar com um nome bem comprido"
	api.shuffle, api.repeat = true, "track"
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	for _, notice := range []string{"", "Fail to change the volume: Player command failed: Cannot control device volume"} {
		m := h.player()
		m.playerBar.notice = barNotice{seq: 1, text: notice, err: true}
		h.model = m
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
						t.Fatalf("%dx%d notice %q: line %d is %d columns wide, want %d:\n%s", width, height, notice, i, got, width, v)
					}
				}
				wantFrameEdges(t, v, width)
			}
		}
	}
}

// wantFrameEdges fails unless every line of the view v, from the tab row to
// the bottom line of the frame, ends with the right edge of the frame at
// column width. The view pads lines to the widest one, so a line that is too
// short still has the right width, with spaces after its edge.
func wantFrameEdges(t *testing.T, v string, width int) {
	t.Helper()

	lines := strings.Split(v, "\n")
	bottom := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "└") {
			bottom = i
		}
	}
	if bottom < 0 {
		t.Fatalf("view has no bottom line:\n%s", v)
	}
	for i := 2; i <= bottom; i++ {
		line := strings.TrimRight(lines[i], " ")
		if got := ansi.StringWidth(line); got != width || !strings.ContainsAny(line[len(line)-len("│"):], "│┤┘┐") {
			t.Fatalf("line %d ends at column %d, want the edge of the frame at %d:\n%s", i, got, width, v)
		}
	}
}

func TestProgressRowDropsControlsThatDoNotFit(t *testing.T) {
	now := time.Now()
	status := playingStatus(0, "", media.Info{Title: "So What", Duration: 9 * time.Minute, Elapsed: time.Minute, Playing: true}, now)
	m := Model{playerBar: playerBar{device: "phone", volume: 60, hasVolume: true, repeat: spotify.RepeatOff}}

	for _, tt := range []struct {
		width int
		want  string
	}{
		{80, "⇄  ↻   🔉 " + strings.Repeat("━", 6) + strings.Repeat("─", 4) + " 60%"},
		{50, "⇄  ↻   🔉 60%"},
		{40, "1:00 / 9:00   🔉 60%"},
		{30, "1:00 / 9:00"},
		{14, "1:00 / 9:00"},
	} {
		row := ansi.Strip(m.progressRow(status, tt.width, now))
		if got := ansi.StringWidth(row); got != tt.width {
			t.Fatalf("progressRow(%d) = %q is %d columns wide", tt.width, row, got)
		}
		if !strings.HasSuffix(strings.TrimRight(row, " "), tt.want) {
			t.Errorf("progressRow(%d) = %q, want it to end with %q", tt.width, row, tt.want)
		}
		if bar := strings.Contains(row, "─ 1:00"); bar != (tt.width >= 30) {
			t.Errorf("progressRow(%d) = %q, want a progress bar: %t", tt.width, row, tt.width >= 30)
		}
	}
}

func TestEqualizer(t *testing.T) {
	now := time.Now()
	playing := track{info: media.Info{Title: "So What", Duration: 9 * time.Minute, Playing: true}, at: now}

	var frames []string
	for second := range len(equalizerFrames) + 1 {
		playing.info.Elapsed = time.Duration(second)*time.Second + 500*time.Millisecond
		frames = append(frames, ansi.Strip(equalizer(playing, now)))
	}
	if !slices.Equal(frames[:len(equalizerFrames)], equalizerFrames) || frames[len(equalizerFrames)] != equalizerFrames[0] {
		t.Fatalf("frames = %q, want %q and then the first again", frames, equalizerFrames)
	}

	paused := playing
	paused.info.Playing = false
	if got := ansi.Strip(equalizer(paused, now)); got != quietEqualizer {
		t.Fatalf("equalizer while paused = %q, want %q", got, quietEqualizer)
	}
	for _, frame := range append(equalizerFrames, quietEqualizer) {
		if got := lipgloss.Width(frame); got != equalizerWidth {
			t.Fatalf("frame %q is %d columns wide, want %d", frame, got, equalizerWidth)
		}
	}
}

func TestEqualizerDancesWithTheClock(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.send(pollPlayback{})
	before := h.playerRows()[0]

	h.agePlaylistStatus(time.Second)

	if after := h.playerRows()[0]; after == before || !strings.HasPrefix(after, equalizerFrames[1]) {
		t.Fatalf("player row 0 a second later = %q, want the next frame %q (it was %q)", after, equalizerFrames[1], before)
	}
}

func TestWideBar(t *testing.T) {
	for _, tt := range []struct {
		value, total int64
		width        int
		want         string
	}{
		{0, 100, 10, "──────────"},
		{4, 100, 10, "──────────"},
		{5, 100, 10, "╸─────────"},
		{50, 100, 10, "━━━━━─────"},
		{55, 100, 10, "━━━━━╸────"},
		{99, 100, 10, "━━━━━━━━━╸"},
		{100, 100, 10, "━━━━━━━━━━"},
		{200, 100, 10, "━━━━━━━━━━"},
		{-5, 100, 10, "──────────"},
		{5, 0, 4, "────"},
		{1, 3, 3, "━──"},
		{50, 100, 0, ""},
	} {
		if got := ansi.Strip(wideBar(tt.value, tt.total, tt.width)); got != tt.want {
			t.Errorf("wideBar(%d, %d, %d) = %q, want %q", tt.value, tt.total, tt.width, got, tt.want)
		}
	}
}

func TestWideBarStaysReadableWithFewColors(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.ANSI} {
		if profile.Convert(styles.LipglossColorGrey) == profile.Convert(styles.LipglossColorForegroundGreenSpotify) {
			t.Errorf("%v: the rest of the bar and the modes that are off look like the part played and the modes that are on", profile)
		}
	}
}

func TestVolumeMeter(t *testing.T) {
	for _, tt := range []struct {
		bar         playerBar
		long, short string
	}{
		{playerBar{volume: 60, hasVolume: true}, "🔉 ━━━━━━──── 60%", "🔉 60%"},
		{playerBar{volume: 0, hasVolume: true}, "🔇 ────────── 0%", "🔇 0%"},
		{playerBar{volume: 20, hasVolume: true}, "🔈 ━━──────── 20%", "🔈 20%"},
		{playerBar{volume: 100, hasVolume: true}, "🔊 ━━━━━━━━━━ 100%", "🔊 100%"},
		{playerBar{volume: 40}, "", ""},
	} {
		if got := ansi.Strip(tt.bar.volumeMeter(true)); got != tt.long {
			t.Errorf("volumeMeter(true) of %+v = %q, want %q", tt.bar, got, tt.long)
		}
		if got := ansi.Strip(tt.bar.volumeMeter(false)); got != tt.short {
			t.Errorf("volumeMeter(false) of %+v = %q, want %q", tt.bar, got, tt.short)
		}
	}
}

func TestModes(t *testing.T) {
	lit, off := playingStyle.Render, dimStyle.Render
	for _, tt := range []struct {
		bar  playerBar
		want string
	}{
		{playerBar{}, ""},
		{playerBar{repeat: spotify.RepeatOff}, off("⇄") + "  " + off("↻")},
		{playerBar{shuffle: true, repeat: spotify.RepeatOff}, lit("⇄") + "  " + off("↻")},
		{playerBar{repeat: spotify.RepeatContext}, off("⇄") + "  " + lit("↻")},
		{playerBar{shuffle: true, repeat: spotify.RepeatTrack}, lit("⇄") + "  " + lit("↻¹")},
		{playerBar{repeat: "something new"}, off("⇄") + "  " + off("↻")},
	} {
		if got := tt.bar.modes(); got != tt.want {
			t.Errorf("modes of %+v = %q, want %q", tt.bar, got, tt.want)
		}
	}
}

func TestPlayerShowsWhereSpotifyPlays(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.wantPlayerRow(0, "on Pixel 7")

	api.set(func(f *fakeAPI) { f.device, f.deviceName = testDeviceID, "spot-go@laptop" })
	h.send(pollPlayback{})
	h.dontWantPlayerRow(0, " on ")

	api.set(func(f *fakeAPI) { f.device, f.deviceName = "tv", "" })
	h.send(pollPlayback{})
	h.dontWantPlayerRow(0, " on ")
}

func TestPlayerSaysWhenNothingPlays(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.dontWantPlayerRow(0, nothingPlaying)

	h.send(pollPlayback{})
	h.wantPlayerRow(0, nothingPlaying)
	h.wantBottomLine("space play/pause")

	h.send(tea.WindowSizeMsg{Width: 120, Height: 12})
	h.wantBottomLine(nothingPlaying)
}

func TestPlayerDoesNotSayNothingPlaysRightAfterAPlayStarts(t *testing.T) {
	api := newFakeAPI()
	api.deviceName = "Pixel 7"
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.wantPlayerRow(0, nothingPlaying)

	h.send(key("enter"))
	h.openSearchTab()
	h.dontWantPlayerRow(0, nothingPlaying, " on ")

	// Spotify can take a moment to report the play.
	h.send(playbackPolled{plays: *h.player().plays})
	h.dontWantPlayerRow(0, nothingPlaying)

	m := h.player()
	m.nowPlaying.commandAt = m.nowPlaying.commandAt.Add(-followGrace)
	h.model = m
	h.send(playbackPolled{plays: *h.player().plays})
	h.wantPlayerRow(0, nothingPlaying)
}

func TestPlayerFollowsWhatAConfirmedPlayReports(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()
	h.send(pollPlayback{})
	h.wantPlayerRow(0, "on Pixel 7")

	api.onPlay = func(f *fakeAPI) {
		backInBlackPlays(f)
		f.device, f.deviceName, f.shuffle, f.repeat = testDeviceID, "spot-go", true, "context"
	}
	h.send(key("enter"))

	h.wantPlayerRow(0, "Back in Black")
	h.dontWantPlayerRow(0, " on ")
	if got := h.player().playerBar; !got.shuffle || got.repeat != spotify.RepeatContext {
		t.Fatalf("player = %+v, want shuffle and repeat as the confirmation reports them", got)
	}
}

func TestPlayerHidesWhatSpotifyNoLongerReports(t *testing.T) {
	api := soWhatOnPixel()
	api.shuffle = true
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.wantPlayerRow(0, "on Pixel 7")
	h.wantPlayerRow(1, "⇄", "60%")

	api.playsElsewhere("", "", "", "")
	h.send(pollPlayback{})

	h.dontWantPlayerRow(0, "on Pixel 7")
	h.dontWantPlayerRow(1, "⇄", "60%")
}

func TestPlayerShowsTheVolumeSpotifyReports(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.dontWantPlayerRow(1, "%")

	h.send(pollPlayback{})
	h.wantVolumeMeter(60)

	api.set(func(f *fakeAPI) { f.volume = 30 })
	h.send(pollPlayback{})
	h.wantVolumeMeter(30)

	api.set(func(f *fakeAPI) { f.noVolume = true })
	h.send(pollPlayback{})
	h.dontWantPlayerRow(1, "%")
}

func TestPolledVolumeRightAfterAVolumeKeyKeepsTheNewVolume(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	h.wantVolumeMeter(65)

	api.set(func(f *fakeAPI) { f.volume = 60 })
	h.send(pollPlayback{})
	h.wantVolumeMeter(65)

	h.forgetVolume()
	h.send(pollPlayback{})
	h.wantVolumeMeter(60)
}

func TestPlayerShowsTheKeysOfEveryTab(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(keys("left", "left")...)
	want := "└─ space play/pause • n/p next/previous • +/- volume • </> seek • ctrl+s shuffle • ctrl+r repeat ─"

	for tab := range h.player().tabs {
		if got := h.player().activeTab; got != tab {
			t.Fatalf("activeTab = %d, want %d", got, tab)
		}
		if got := h.bottomLine(); !strings.HasPrefix(got, want) {
			t.Fatalf("%s tab: bottom line = %q, want %q", h.player().tabs[tab].name, got, want)
		}
		h.send(key("right"))
	}
}

func TestCompactPlayerLeavesTheKeysToThePane(t *testing.T) {
	h := newHarness(t, 120, 15)

	h.wantView("enter play • r refresh • space play/pause • n/p next/previous")
	if got := h.bottomLine(); strings.Contains(got, "play/pause") {
		t.Fatalf("bottom line = %q, want no keys in the compact player", got)
	}
	for _, line := range strings.Split(h.view(), "\n") {
		if strings.Contains(line, "r refresh") && strings.Contains(line, "…") {
			t.Fatalf("keys line = %q, want the keys that fit, without cutting one", line)
		}
	}

	h.send(tea.WindowSizeMsg{Width: 120, Height: 16})
	h.wantBottomLine("space play/pause")
	h.dontWantView("r refresh • space")
}

func TestPlayerLeavesOutKeysThatDoNotFit(t *testing.T) {
	h := newHarness(t, 80, 24)

	h.wantBottomLine("space play/pause • n/p next/previous • +/- volume • </> seek")
	if got := h.bottomLine(); strings.Contains(got, "ctrl+") || strings.Contains(got, "…") {
		t.Fatalf("bottom line = %q, want the keys that fit, without cutting one", got)
	}

	h.send(key("tab"))
	h.wantView("tab playlists • enter play • r refresh")
}

func TestNextAndPreviousKeys(t *testing.T) {
	api := soWhatOnPhone()
	api.nextItem, api.nextName = "spotify:track:paranoid", "Paranoid"
	h := newHarnessWith(t, api, 120, 40)

	h.send(key("n"))
	h.wantSkips("next")
	h.wantPlayerRow(0, "⏸ Paranoid")

	h.send(key("p"))
	h.wantSkips("next", "previous")
}

func TestNextAndPreviousKeysWhileFilteringAreTyped(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)

	h.send(keys("/", "n", "p", "<", ">", ",", ".")...)

	if got := h.playlists().list.FilterValue(); got != "np<>,." {
		t.Fatalf("filter = %q, want %q", got, "np<>,.")
	}
	h.wantSkips()
	h.wantSeeks()
	h.wantPlayerReads(0)
}

func TestSeekKeys(t *testing.T) {
	for _, tt := range []struct {
		key   string
		want  string
		clock string
	}{
		{">", "70000", "1:10 / 9:22"},
		{".", "70000", "1:10 / 9:22"},
		{"<", "50000", "0:50 / 9:22"},
		{",", "50000", "0:50 / 9:22"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
			published := h.recordNowPlaying()

			h.send(key(tt.key))

			h.wantSeeks(tt.want)
			h.wantPlayerReads(1)
			h.wantPlayerRow(0, "⏸ So What — Miles Davis")
			h.wantPlayerRow(1, tt.clock)
			if got := lastInfo(t, published); got.Title != "So What" || !got.Playing || fmt.Sprint(got.Elapsed.Milliseconds()) != tt.want {
				t.Fatalf("now playing = %+v, want So What playing from %s ms", got, tt.want)
			}
		})
	}
}

func TestSeekStopsAtTheStartAndTheEnd(t *testing.T) {
	for _, tt := range []struct {
		progressMs int
		key        string
		want       string
	}{
		{4000, "<", "0"},
		{559000, ">", "561000"},
	} {
		api := soWhatOnPhone()
		api.progressMs = tt.progressMs
		h := newHarnessWith(t, api, 120, 40)

		h.send(key(tt.key))

		h.wantSeeks(tt.want)
	}
}

func TestSeekWhilePausedStaysPaused(t *testing.T) {
	api := soWhatOnPhone()
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)

	h.send(key(">"))

	h.wantSeeks("70000")
	h.wantPlayerRow(0, "▶ So What")
	h.wantPlayerRow(1, "1:10 / 9:22")
	h.wantPlays(0)
	h.wantPauses()
}

func TestSeekRightAfterASeekUsesTheKnownPosition(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)

	for _, k := range []string{">", ">", "<"} {
		h.send(key(k))
	}

	h.wantPlayerReads(1)
	got := h.api.seekRequests()
	var ms []int
	for _, s := range got {
		n, _ := strconv.Atoi(s)
		ms = append(ms, n)
	}
	if len(ms) != 3 || ms[0] != 70000 || ms[1] < 80000 || ms[1] >= 81000 || ms[2] < 70000 || ms[2] >= 71000 {
		t.Fatalf("seek requests = %q, want 70 s, then 80 s and 70 s from the known position", got)
	}
}

func TestSeekLaterReadsThePositionAgain(t *testing.T) {
	api := soWhatOnPhone()
	h := newHarnessWith(t, api, 120, 40)

	h.send(key(">"))
	h.forgetToggle()
	api.set(func(f *fakeAPI) { f.progressMs = 300000 })
	h.send(key(">"))

	h.wantPlayerReads(2)
	h.wantSeeks("70000", "310000")
}

func TestSeekPressesWhileChangingAddUp(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)

	model, toggle := h.model.Update(key("space"))
	for _, k := range []string{">", ">", "<", ">"} {
		var cmd tea.Cmd
		model, cmd = model.Update(key(k))
		if cmd != nil {
			t.Fatalf("press %q while pausing sent a request", k)
		}
	}
	h.model = model
	h.run(toggle)

	h.wantPauses("")
	h.wantPlayerReads(1)
	h.wantSeeks("80000")
	h.wantPlayerRow(0, "▶ So What")
	h.wantPlayerRow(1, "1:20 / 9:22")
}

func TestSeekAfterSkipsPressedWhileChanging(t *testing.T) {
	api := soWhatOnPhone()
	api.nextItem, api.nextName = "spotify:track:paranoid", "Paranoid"
	h := newHarnessWith(t, api, 120, 40)
	h.confirmStarts()

	model, toggle := h.model.Update(key("space"))
	model, _ = model.Update(key("n"))
	model, _ = model.Update(key(">"))
	h.model = model
	h.run(toggle)

	h.wantSkips("next")
	h.wantSeeks("10000")
	h.wantPlayerRow(0, "Paranoid")
}

func TestSeekPressedBeforeASkipIsDropped(t *testing.T) {
	api := soWhatOnPhone()
	api.nextItem, api.nextName = "spotify:track:paranoid", "Paranoid"
	h := newHarnessWith(t, api, 120, 40)

	model, toggle := h.model.Update(key("space"))
	for _, k := range []string{">", ">", "n", "<"} {
		model, _ = model.Update(key(k))
	}
	h.model = model
	h.run(toggle)

	h.wantSkips("next")
	h.wantSeeks("0")
}

func TestPlayStartedAfterPressesWhileChangingDropsThem(t *testing.T) {
	h := newHarnessWith(t, playingOnPhone(), 120, 40)

	model, toggle := h.model.Update(key("space"))
	for _, k := range []string{"n", ">"} {
		model, _ = model.Update(key(k))
	}
	h.model = model
	h.send(key("enter"))
	h.run(toggle)

	h.wantSkips()
	h.wantSeeks()
	h.wantPlayerRow(0, "⏸ Rock")
}

func TestSeekNearTheEndStopsShortOfTheNextSong(t *testing.T) {
	api := soWhatOnPhone()
	api.progressMs = 555000
	api.nextItem, api.nextName = "spotify:track:paranoid", "Paranoid"
	h := newHarnessWith(t, api, 120, 40)

	h.send(key(">"))
	h.wantSeeks("561000")

	// A second later, the song ended, so the next seek reads what plays.
	m := h.player()
	m.toggle.at = m.toggle.at.Add(-1500 * time.Millisecond)
	h.model = m
	api.set(func(f *fakeAPI) {
		f.playingItem, f.playingName, f.progressMs = "spotify:track:paranoid", "Paranoid", 1000
	})
	h.send(key(">"))

	h.wantPlayerReads(2)
	h.wantSeeks("561000", "11000")
	h.wantPlayerRow(0, "Paranoid")
}

func TestSeekAfterASkipSpotifyDidNotReportReadsThePositionAgain(t *testing.T) {
	api := playingOnPhone()
	h := newHarnessWith(t, api, 120, 40)

	h.send(media.NextTrack)
	h.wantPlayerReads(1 + confirmPolls)
	api.set(func(f *fakeAPI) { f.progressMs = 3000 })
	h.send(key(">"))

	h.wantPlayerReads(2 + confirmPolls)
	h.wantSeeks("13000")
}

func TestSeekWithNothingPlaying(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.holdTimers = true

	h.send(key(">"))

	h.wantSeeks()
	h.wantPlayerRow(0, "Fail to seek: nothing is playing")

	h.holdTimers = false
	h.send()
	h.dontWantPlayerRow(0, "Fail to seek")
}

func TestSeekFailure(t *testing.T) {
	api := soWhatOnPhone()
	api.seekStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	model, seek := h.model.Update(key(">"))
	model, _ = model.Update(key(">"))
	h.model = model
	h.run(seek)

	h.wantSeeks("70000")
	h.wantPlayerRow(0, "⏸ So What")
	h.wantPlayerRow(0, "Fail to seek: Player command failed: Restriction violated")
	h.send(key("right"))
	h.wantPlayerRow(0, "Fail to seek")
	h.holdTimers = false
	h.send()
	h.dontWantPlayerRow(0, "Fail to seek")
	h.send(key("left"))
	h.wantPlayerRow(0, "⏸ So What")
	h.dontWantPlayerRow(0, "Fail to seek")

	api.set(func(f *fakeAPI) { f.seekStatus = 0 })
	h.send(key(">"))
	h.wantSeeks("70000", "70000")
	h.wantPlayerRow(0, "⏸ So What")
}

func TestShuffleKey(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})
	h.wantPlayerRow(1, "⇄  ↻")
	reads := api.playerReads()

	h.send(key("ctrl+s"))
	h.wantModes("shuffle=true")
	h.wantPlayerRow(0, "Shuffle on")
	if row := h.player().playerBar.modes(); !strings.HasPrefix(row, playingStyle.Render(shuffleIcon)) {
		t.Fatalf("modes = %q, want shuffle lit", row)
	}

	h.send(key("ctrl+s"))
	h.wantModes("shuffle=true", "shuffle=false")
	h.wantPlayerRow(0, "Shuffle off")
	if got := api.playerReads() - reads; got != 1 {
		t.Fatalf("player reads = %d, want 1: the second press uses what the first one set", got)
	}

	h.holdTimers = false
	h.send()
	h.dontWantPlayerRow(0, "Shuffle")
	h.wantPlayerRow(0, "on Pixel 7")
}

func TestRepeatKeyGoesThroughTheModes(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	for _, want := range []struct{ request, notice, icon string }{
		{"repeat=context", "Repeat on", "↻"},
		{"repeat=track", "Repeat one", "↻¹"},
		{"repeat=off", "Repeat off", "↻"},
	} {
		h.send(key("ctrl+r"))
		if got := h.api.modeRequests(); got[len(got)-1] != want.request {
			t.Fatalf("mode requests = %q, want %q last", got, want.request)
		}
		h.wantPlayerRow(0, want.notice)
		h.wantPlayerRow(1, "⇄  "+want.icon)
	}
	h.wantPlayerReads(1)
}

// soWhatOnSpotGo is So What playing on spot-go's own device, Soloist.
func soWhatOnSpotGo() *fakeAPI {
	api := soWhatOnPhone()
	api.device, api.deviceName = testDeviceID, "spot-go@laptop"
	return api
}

func (f *fakeAPI) soloistModeRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.soloistModes...)
}

func TestModesOnSpotGoChangeThroughSoloist(t *testing.T) {
	api := soWhatOnSpotGo()
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("ctrl+s"))
	h.wantPlayerRow(0, "Shuffle on")
	h.send(key("ctrl+r"))
	h.wantPlayerRow(0, "Repeat on")
	h.send(key("ctrl+r"))
	h.wantPlayerRow(0, "Repeat one")
	h.wantPlayerRow(1, "⇄  ↻¹")

	h.wantModes()
	if got, want := api.soloistModeRequests(), []string{"shuffle=true", "repeat=context", "repeat=track"}; !slices.Equal(got, want) {
		t.Fatalf("Soloist mode changes = %q, want %q", got, want)
	}

	// Spotify then reports what Soloist set.
	h.holdTimers = false
	h.send()
	h.forgetModes()
	h.send(pollPlayback{})
	if b := h.player().playerBar; !b.shuffle || b.repeat != spotify.RepeatTrack {
		t.Fatalf("player = %+v, want shuffle on and repeat one", b)
	}
}

func TestModesFollowWhereSpotifyPlays(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.send(key("ctrl+s"))
	h.forgetModes()

	api.set(func(f *fakeAPI) { f.device, f.deviceName = testDeviceID, "spot-go@laptop" })
	h.send(key("ctrl+s"))

	h.wantModes("shuffle=true")
	if got := api.soloistModeRequests(); !slices.Equal(got, []string{"shuffle=false"}) {
		t.Fatalf("Soloist mode changes = %q, want shuffle off, as Spotify moved to spot-go", got)
	}
}

func TestModesFollowAMoveRightAfterAChange(t *testing.T) {
	for _, tt := range []struct {
		name         string
		api          func() *fakeAPI
		move         func(h *harness)
		web, soloist []string
	}{
		{
			name: "to spot-go",
			api:  soWhatOnPixel,
			move: func(h *harness) {
				h.api.set(func(f *fakeAPI) { f.device, f.deviceName = testDeviceID, "spot-go@laptop" })
				h.send(pollPlayback{})
			},
			web: []string{"shuffle=true"}, soloist: []string{"shuffle=false"},
		},
		{
			name: "to the phone",
			api:  soWhatOnSpotGo,
			move: func(h *harness) {
				h.api.set(func(f *fakeAPI) { f.device, f.deviceName = "phone", "Pixel 7" })
				h.send(pollPlayback{})
			},
			web: []string{"shuffle=false"}, soloist: []string{"shuffle=true"},
		},
		{
			name: "play on spot-go",
			api:  soWhatOnPixel,
			move: func(h *harness) {
				h.api.set(func(f *fakeAPI) {
					f.onPlay = func(f *fakeAPI) { backInBlackPlays(f); f.device = testDeviceID }
				})
				h.send(key("enter"))
			},
			web: []string{"shuffle=true"}, soloist: []string{"shuffle=false"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := tt.api()
			h := newHarnessWith(t, api, 120, 40)
			h.holdTimers = true
			h.send(pollPlayback{})

			h.send(key("ctrl+s"))
			tt.move(h)
			h.send(key("ctrl+s"))

			h.wantModes(tt.web...)
			if got := api.soloistModeRequests(); !slices.Equal(got, tt.soloist) {
				t.Fatalf("Soloist mode changes = %q, want %q", got, tt.soloist)
			}
			h.wantPlayerRow(0, "Shuffle off")
		})
	}
}

func TestModeFailureOnSpotGo(t *testing.T) {
	api := soWhatOnSpotGo()
	api.soloistErr = errors.New("Spotify Soloist is not running with --ws")
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("ctrl+r"))

	h.wantModes()
	h.wantPlayerRow(0, "Fail to change repeat: Spotify Soloist is not running with --ws")
	if got := h.player().playerBar.repeat; got != spotify.RepeatOff {
		t.Fatalf("repeat = %q, want it still off", got)
	}
}

func TestModePressesWhileChangingAddUp(t *testing.T) {
	for _, tt := range []struct {
		key     string
		presses int
		want    []string
	}{
		{"ctrl+s", 1, []string{"shuffle=true", "shuffle=false"}},
		{"ctrl+s", 2, []string{"shuffle=true"}},
		{"ctrl+s", 3, []string{"shuffle=true", "shuffle=false"}},
		{"ctrl+r", 1, []string{"repeat=context", "repeat=track"}},
		{"ctrl+r", 2, []string{"repeat=context", "repeat=off"}},
		{"ctrl+r", 3, []string{"repeat=context"}},
	} {
		h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
		h.holdTimers = true

		model, change := h.model.Update(key(tt.key))
		for range tt.presses {
			var cmd tea.Cmd
			model, cmd = model.Update(key(tt.key))
			if cmd != nil {
				t.Fatalf("%s pressed while changing sent a request", tt.key)
			}
		}
		h.model = model
		h.run(change)

		h.wantModes(tt.want...)
		h.wantPlayerReads(1)
	}
}

func TestModeKeysWorkOnEveryTab(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)
	h.holdTimers = true

	h.send(keys("left", "left")...)
	for range h.player().tabs {
		h.forgetModes()
		h.send(key("ctrl+s"))
		h.send(key("right"))
	}

	h.wantModes("shuffle=true", "shuffle=false", "shuffle=true", "shuffle=false", "shuffle=true", "shuffle=false")
}

func TestModeKeysWhileFilteringDoNothing(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPixel(), 120, 40)

	h.send(keys("/", "ctrl+s", "ctrl+r")...)

	h.wantModes()
	h.wantPlayerReads(0)
}

func TestModeWithNothingPlaying(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.holdTimers = true

	h.send(key("ctrl+s"))

	h.wantModes()
	h.wantPlayerRow(0, "Fail to change shuffle: no active device")
}

func TestModeFailure(t *testing.T) {
	api := soWhatOnPixel()
	api.modeStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	model, change := h.model.Update(key("ctrl+r"))
	model, _ = model.Update(key("ctrl+r"))
	h.model = model
	h.run(change)

	h.wantModes("repeat=context")
	h.wantPlayerRow(0, "Fail to change repeat: Player command failed: Restriction violated")
	h.wantPlayerRow(1, "⇄  ↻")
	if got := h.player().playerBar.repeat; got != spotify.RepeatOff {
		t.Fatalf("repeat = %q, want it still off", got)
	}

	h.holdTimers = false
	h.send()
	h.dontWantPlayerRow(0, "Fail to change repeat")
	h.wantPlayerRow(0, "⏸ So What")
}

func TestPollRightAfterAModeChangeKeepsTheNewMode(t *testing.T) {
	api := soWhatOnPixel()
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("ctrl+s"))
	api.set(func(f *fakeAPI) { f.shuffle = false })
	h.send(pollPlayback{})
	if !h.player().playerBar.shuffle {
		t.Fatal("a poll right after turning shuffle on turned it off")
	}

	h.forgetModes()
	h.send(pollPlayback{})
	if h.player().playerBar.shuffle {
		t.Fatal("a poll a while after turning shuffle on didn't show it off")
	}
}

func TestOlderNoticeTimerKeepsTheNewNotice(t *testing.T) {
	api := soWhatOnPixel()
	api.volumeStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 40)
	h.holdTimers = true

	h.send(key("+"))
	first := h.timers
	h.timers = nil
	h.send(key("ctrl+s"))
	h.send(first...)

	h.wantPlayerRow(0, "Shuffle on")

	h.holdTimers = false
	h.send()
	h.dontWantPlayerRow(0, "Shuffle on", "Fail")
}

func TestNoticeShowsInTheCompactPlayer(t *testing.T) {
	api := soWhatOnPixel()
	api.volumeStatus = http.StatusForbidden
	h := newHarnessWith(t, api, 120, 14)
	h.holdTimers = true
	h.send(pollPlayback{})

	h.send(key("+"))

	line := h.bottomLine()
	if !strings.Contains(line, "⏸ So What") || !strings.HasSuffix(line, " Fail to change the volume: Player command failed: Cannot control device volume ─┘") {
		t.Fatalf("bottom line = %q, want the song and the notice", line)
	}
}
