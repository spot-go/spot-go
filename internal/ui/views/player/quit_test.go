package player

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// quits reports whether cmd quits spot-go.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if quits(c) {
				return true
			}
		}
	}
	return false
}

// press sends the key k to spot-go, and reports whether it quit.
func (h *harness) press(k string) bool {
	h.t.Helper()

	m, cmd := h.model.Update(key(k))
	h.model = m
	h.view()
	return quits(cmd)
}

func (h *harness) wantQuitPrompt() {
	h.t.Helper()

	if !h.player().quitPrompt.open {
		h.t.Fatalf("the quit prompt is closed:\n%s", h.view())
	}
	h.wantView(quitQuestion, "Keep playing", "Stop", "Cancel")
}

func (h *harness) wantNoQuitPrompt() {
	h.t.Helper()

	if h.player().quitPrompt.open {
		h.t.Fatalf("the quit prompt is open:\n%s", h.view())
	}
	h.dontWantView(quitQuestion)
}

func TestQuitKeysAskWhilePlayingOnSpotGo(t *testing.T) {
	for _, k := range []string{"ctrl+c", "ctrl+q"} {
		t.Run(k, func(t *testing.T) {
			h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
			h.send(pollPlayback{})

			if h.press(k) {
				t.Fatal("spot-go quit without asking whether the music keeps playing")
			}
			h.wantQuitPrompt()
			// The player still shows what plays.
			h.wantPlayerRow(0, "So What")
		})
	}
}

func TestQuitKeysQuitRightAwayWhenSpotGoPlaysNothing(t *testing.T) {
	tests := []struct {
		name string
		api  func() *fakeAPI
	}{
		{name: "nothing plays", api: newFakeAPI},
		{name: "plays on another device", api: soWhatOnPixel},
		{name: "paused on spot-go", api: func() *fakeAPI {
			api := soWhatOnSpotGo()
			api.paused = true
			return api
		}},
	}
	for _, tt := range tests {
		for _, k := range []string{"ctrl+c", "ctrl+q"} {
			t.Run(tt.name+"/"+k, func(t *testing.T) {
				h := newHarnessWith(t, tt.api(), 120, 40)
				h.send(pollPlayback{})

				if !h.press(k) {
					t.Fatal("spot-go did not quit")
				}
				if h.player().KeepPlaying() {
					t.Fatal("KeepPlaying() = true without asking")
				}
			})
		}
	}
}

func TestQuitPromptChoices(t *testing.T) {
	tests := []struct {
		keys []string
		quit bool
		keep bool
	}{
		{keys: []string{"enter"}, quit: true, keep: true},
		{keys: []string{"k"}, quit: true, keep: true},
		{keys: []string{"s"}, quit: true},
		{keys: []string{"right", "enter"}, quit: true},
		{keys: []string{"right", "right", "enter"}},
		{keys: []string{"right", "right", "right", "enter"}},
		{keys: []string{"left", "enter"}, quit: true, keep: true},
		{keys: []string{"right", "left", "enter"}, quit: true, keep: true},
		{keys: []string{"tab", "tab", "tab", "enter"}, quit: true, keep: true},
		{keys: []string{"shift+tab", "enter"}},
		{keys: []string{"esc"}},
		{keys: []string{"ctrl+c"}, quit: true},
		{keys: []string{"ctrl+q"}, quit: true},
		{keys: []string{"right", "right", "k"}, quit: true, keep: true},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
			h.send(pollPlayback{})
			h.press("ctrl+c")

			quit := false
			for _, k := range tt.keys {
				if quit {
					t.Fatalf("spot-go quit before %q", k)
				}
				quit = h.press(k)
			}
			if quit != tt.quit {
				t.Fatalf("quit = %t, want %t", quit, tt.quit)
			}
			if got := h.player().KeepPlaying(); got != tt.keep {
				t.Fatalf("KeepPlaying() = %t, want %t", got, tt.keep)
			}
			if !tt.quit {
				h.wantNoQuitPrompt()
			}
		})
	}
}

func TestQuitPromptLightsTheChosenButton(t *testing.T) {
	q := quitPrompt{open: true}
	for choice := range quitChoiceCount {
		q.choice = choice
		want := quitChosenStyle.Render(quitChoiceNames[choice])
		if got := q.choices(); !strings.Contains(got, want) {
			t.Fatalf("choices() with %s chosen = %q, want %q in it", quitChoiceNames[choice], got, want)
		}
	}
}

func TestQuitPromptTakesEveryKey(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.send(pollPlayback{})
	h.press("ctrl+c")

	for _, k := range []string{"space", "n", "p", "+", "-", ">", "<", "ctrl+s", "ctrl+r", "/", "r"} {
		if h.press(k) {
			t.Fatalf("%q quit spot-go", k)
		}
	}
	h.wantPauses()
	h.wantSkips()
	h.wantVolumes("")
	h.wantSeeks()
	h.wantModes()
	if tab := h.player().activeTab; tab != playlistsTab {
		t.Fatalf("active tab = %d, want the prompt to keep %d", tab, playlistsTab)
	}

	h.press("esc")
	h.wantNoQuitPrompt()
	h.send(key("right"))
	if tab := h.player().activeTab; tab != podcastsTab {
		t.Fatalf("active tab = %d after the prompt closed, want %d", tab, podcastsTab)
	}
	if h.press("ctrl+c") {
		t.Fatal("spot-go quit without asking again")
	}
	h.wantQuitPrompt()
}

func TestQuitPromptWhileTypingASearch(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.send(pollPlayback{})
	for h.player().activeTab > searchTab {
		h.send(key("left"))
	}
	h.send(key("/"))

	if h.press("ctrl+c") {
		t.Fatal("spot-go quit without asking while typing a search")
	}
	h.wantQuitPrompt()
	h.press("esc")
	h.wantNoQuitPrompt()
}

func TestQuitAsksRightAfterResumingWithNoActiveDevice(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(pollPlayback{})
	h.send(key("space"))

	if h.press("ctrl+c") {
		t.Fatal("spot-go quit without asking right after it resumed on its own device")
	}
	h.wantQuitPrompt()
}

func TestQuitPromptFitsEveryWindow(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.send(pollPlayback{})
	h.press("ctrl+c")

	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 24}, {40, 24}, {120, 12}, {120, 10}, {30, 8}, {120, 4}, {10, 3}} {
		width, height := size[0], size[1]
		h.send(tea.WindowSizeMsg{Width: width, Height: height})
		v := h.view()
		if !strings.Contains(v, "Keep") {
			t.Fatalf("%dx%d doesn't show the prompt:\n%s", width, height, v)
		}
		if strings.Contains(v, tooSmallWindow) {
			t.Fatalf("%dx%d shows the too small message instead of the prompt:\n%s", width, height, v)
		}
		lines := strings.Split(v, "\n")
		if len(lines) > height {
			t.Fatalf("%dx%d: the view has %d lines:\n%s", width, height, len(lines), v)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("%dx%d: line %d is %d columns wide:\n%s", width, height, i, got, v)
			}
		}
	}

	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	h.wantView("╭", quitQuestion, quitDetail, "k keep playing • s stop • esc cancel")
}

func TestQuitBeforeTheFirstPollAsksWhenSpotGoWasActive(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	// As when an earlier run of spot-go left Soloist playing: Spotify listed
	// spot-go's device as the active one, and the first poll didn't end yet.
	h.player().playerDevice.Active = true

	if h.press("ctrl+c") {
		t.Fatal("spot-go quit before it knew what plays on its active device")
	}
	h.wantQuitPrompt()
	if !h.press("k") || !h.player().KeepPlaying() {
		t.Fatal("k did not keep the music playing")
	}
}

func TestQuitBeforeTheFirstPollQuitsWhenSpotGoWasNotActive(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)

	if !h.press("ctrl+c") {
		t.Fatal("spot-go asked although its device was not active as it started")
	}
}

func TestQuitAfterThePollFollowsWhatPlays(t *testing.T) {
	api := soWhatOnSpotGo()
	api.paused = true
	h := newHarnessWith(t, api, 120, 40)
	h.player().playerDevice.Active = true
	h.send(pollPlayback{})

	if !h.press("ctrl+c") {
		t.Fatal("spot-go asked although the poll found it paused")
	}
}

func TestQuitPromptDropsPastes(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.send(pollPlayback{})
	for h.player().activeTab > searchTab {
		h.send(key("left"))
	}
	h.send(key("/"))
	h.press("ctrl+c")

	h.send(tea.PasteMsg{Content: "pasted"})
	h.press("esc")
	if got := h.player().tabs[searchTab].content.(searchModel).input.Value(); got != "" {
		t.Fatalf("search input = %q after a paste under the quit prompt, want it empty", got)
	}
	h.send(tea.PasteMsg{Content: "pasted"})
	if got := h.player().tabs[searchTab].content.(searchModel).input.Value(); got != "pasted" {
		t.Fatalf("search input = %q after a paste, want the paste: the test doesn't reach the input", got)
	}
}

func TestQuitAsksWhenAPollBeforeAPlayIsDropped(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.player().playerDevice.Active = true
	// A play started while the first poll ran, so the poll says nothing
	// about what plays now, and the play failed.
	plays := *h.player().plays
	*h.player().plays = plays + 1
	h.send(playbackPolled{plays: plays}, searchPlaybackFailed{err: errors.New("Restriction violated")})

	if h.press("ctrl+c") {
		t.Fatal("spot-go quit without asking after a poll it dropped")
	}
	h.wantQuitPrompt()
}
