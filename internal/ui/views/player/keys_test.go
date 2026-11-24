package player

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (h *harness) wantKeyList(texts ...string) {
	h.t.Helper()

	if !h.player().keyList.open {
		h.t.Fatalf("the list of keys is closed:\n%s", h.view())
	}
	h.wantView(append([]string{"╭─ " + keyListTitle + " ─"}, texts...)...)
}

func (h *harness) wantNoKeyList() {
	h.t.Helper()

	if h.player().keyList.open {
		h.t.Fatalf("the list of keys is open:\n%s", h.view())
	}
	h.dontWantView("╭─ " + keyListTitle + " ─")
}

// keyListRows are the lines inside the frame of the list of keys, without
// the frame.
func (h *harness) keyListRows() []string {
	h.t.Helper()

	lines := strings.Split(h.view(), "\n")
	for i, line := range lines {
		top := []rune(line)
		start, end := slices.Index(top, '╭'), slices.Index(top, '╮')
		if !strings.Contains(line, "╭─ "+keyListTitle) || start < 0 || end < start {
			continue
		}
		var rows []string
		for _, line := range lines[i+1:] {
			row := []rune(line)
			if len(row) <= end || row[start] == '╰' {
				return rows
			}
			rows = append(rows, strings.TrimSpace(string(row[start+1:end])))
		}
	}
	h.t.Fatalf("no list of keys:\n%s", h.view())
	return nil
}

func TestQuestionMarkShowsTheKeys(t *testing.T) {
	for _, closer := range []string{"?", "esc"} {
		t.Run(closer, func(t *testing.T) {
			h := newHarness(t, 120, 40)

			h.send(key("?"))
			h.wantKeyList(
				"Playlists tab", "tab", "move between playlists and songs", "sort the playlists",
				"Playback", "space/k", "play/pause", "ctrl+s", "shuffle on/off", "ctrl+r", "next repeat mode",
				"General", "←/→", "switch tabs", "ctrl+c/ctrl+q", "quit",
				"←/→ other tabs • esc close",
			)
			// The list of keys shows over the tab, and the player stays.
			h.dontWantView("Back in Black")
			h.wantBottomLine("space/k play/pause")

			h.send(key(closer))
			h.wantNoKeyList()
			h.wantView("Back in Black")
		})
	}
}

func TestKeyListShowsTheKeysOfEachTab(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.openTab(queueTab)
	h.send(key("?"))

	want := [][]string{
		{"Queue tab", "skip to the selected song or episode", "reload the queue"},
		{"Search tab", "type a search", "search, or play the selected result", "search again"},
		{"Playlists tab", "move between playlists and songs", "filter the playlists", "reload the songs"},
		{"Podcasts tab", "move between podcasts and episodes", "play the newest episode, or the selected one"},
		{"Artists tab", "move between artists and albums", "play the artist, or the selected album"},
		{"Albums tab", "move between albums and songs", "play the album, or from the selected song"},
	}
	for tab, texts := range want {
		if got := h.player().activeTab; got != tab {
			t.Fatalf("activeTab = %d, want %d", got, tab)
		}
		// ←/→ switch tabs under the list, which shows the keys of each.
		h.wantKeyList(append(texts, "Playback", "General")...)
		h.send(key("right"))
	}
	h.wantKeyList("Albums tab")
	h.send(key("left"))
	h.wantKeyList("Artists tab")
}

func TestQuestionMarkIsTypedWhileTyping(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		h := newHarness(t, 120, 40)
		h.openSearchTab()

		h.send(keys("/", "w", "h", "o", "?")...)
		h.wantNoKeyList()
		if got := h.searchTab().input.Value(); got != "who?" {
			t.Fatalf("input = %q, want %q", got, "who?")
		}

		// Once the input is left, ? shows the keys again.
		h.send(keys("esc", "?")...)
		h.wantKeyList("Search tab")
		if got := h.searchTab().input.Value(); got != "" {
			t.Fatalf("input = %q after esc, want it empty", got)
		}
	})

	t.Run("filter", func(t *testing.T) {
		h := newHarness(t, 120, 40)

		h.send(keys("/", "?")...)
		h.wantNoKeyList()
		if got := h.playlists().list.FilterValue(); got != "?" {
			t.Fatalf("filter = %q, want %q", got, "?")
		}

		// With the filter applied, ? shows the keys again.
		h.send(keys("esc", "/", "r", "o", "enter", "?")...)
		h.wantKeyList("Playlists tab")
		if got := h.playlists().list.FilterValue(); got != "ro" {
			t.Fatalf("filter = %q, want %q", got, "ro")
		}

		// The esc that hides the list doesn't reach the tab, where it would
		// clear the filter.
		h.send(key("esc"))
		h.wantNoKeyList()
		if l := h.playlists().list; l.FilterState() != list.FilterApplied || l.FilterValue() != "ro" {
			t.Fatalf("filter = %q (%v) after esc hid the list, want %q applied", l.FilterValue(), l.FilterState(), "ro")
		}
	})
}

func TestKeyListKeepsTheKeysOfTheTab(t *testing.T) {
	h := newHarnessWith(t, soWhatOnPhone(), 120, 40)
	h.send(pollPlayback{})
	before := h.playlists()
	order := before.order()
	h.send(key("?"))

	h.send(keys("tab", "enter", "s", "r", "/", "x", "end", "down", "up")...)
	h.send(tea.PasteMsg{Content: "pasted"})

	p := h.playlists()
	if p.leftFocus != before.leftFocus {
		t.Fatal("tab moved the focus under the list of keys")
	}
	if got := p.order(); got != order {
		t.Fatalf("order = %v under the list of keys, want %v", got, order)
	}
	if p.capturesInput() || p.list.FilterValue() != "" {
		t.Fatalf("filter = %q under the list of keys, want none", p.list.FilterValue())
	}
	if got := len(h.api.playRequests()); got != 0 {
		t.Fatalf("plays = %d under the list of keys, want none", got)
	}
	if got, want := p.list.Index(), before.list.Index(); got != want {
		t.Fatalf("selected playlist = %d under the list of keys, want %d", got, want)
	}
	h.wantKeyList()
}

func TestSpotifyKeysWorkUnderTheKeyList(t *testing.T) {
	api := soWhatOnPhone()
	api.volume = 50
	h := newHarnessWith(t, api, 120, 40)
	h.send(pollPlayback{})
	h.send(key("?"))

	h.send(key("space"))
	h.wantPauses("")
	h.send(key("+"))
	h.wantVolumes("phone", "55")
	h.wantKeyList()
}

func TestQuitUnderTheKeyList(t *testing.T) {
	h := newHarnessWith(t, soWhatOnSpotGo(), 120, 40)
	h.send(pollPlayback{})
	h.send(key("?"))

	if h.press("ctrl+c") {
		t.Fatal("spot-go quit without asking whether the music keeps playing")
	}
	h.wantQuitPrompt()
	h.dontWantView("╭─ " + keyListTitle)

	// Cancel goes back to the list of keys.
	h.press("esc")
	h.wantNoQuitPrompt()
	h.wantKeyList()

	t.Run("quits", func(t *testing.T) {
		h := newHarness(t, 120, 40)
		h.send(key("?"))
		if !h.press("ctrl+q") {
			t.Fatal("ctrl+q did not quit under the list of keys")
		}
	})
}

func TestKeyListScrollsWhenItDoesNotFit(t *testing.T) {
	h := newHarness(t, 80, 60)
	h.send(key("?"))
	all := h.keyListRows()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.wantKeyList("↑/↓ scroll • ←/→ other tabs • esc close")

	first := h.keyListRows()
	if len(first) == 0 || !strings.Contains(first[0], "Playlists tab") {
		t.Fatalf("rows = %q, want the keys of the tab first", first)
	}
	h.dontWantView("ctrl+c/ctrl+q")

	h.send(key("down"))
	if got := h.keyListRows(); got[0] != first[1] {
		t.Fatalf("first row after ↓ = %q, want %q", got[0], first[1])
	}
	h.send(key("up"), key("up"))
	if got := h.keyListRows(); got[0] != first[0] {
		t.Fatalf("first row after ↑ = %q, want %q", got[0], first[0])
	}

	h.send(key("end"))
	last := h.keyListRows()
	if !strings.Contains(last[len(last)-1], "ctrl+c/ctrl+q") {
		t.Fatalf("rows after end = %q, want the last key last", last)
	}
	h.send(key("down"))
	if got := h.keyListRows(); got[0] != last[0] {
		t.Fatalf("first row after ↓ at the end = %q, want %q", got[0], last[0])
	}
	// The first ↑ moves at once, from the end.
	h.send(key("up"))
	if got := h.keyListRows(); got[1] != last[0] {
		t.Fatalf("second row after ↑ at the end = %q, want %q", got[1], last[0])
	}

	h.send(key("home"))
	if got := h.keyListRows(); got[0] != first[0] {
		t.Fatalf("first row after home = %q, want %q", got[0], first[0])
	}
	page, end := len(first), len(all)-len(first)
	h.send(key("pgdown"))
	if got, want := h.keyListRows(), all[min(page, end)]; got[0] != want {
		t.Fatalf("first row after pgdown = %q, want %q, a page further", got[0], want)
	}
	h.send(key("pgup"))
	if got := h.keyListRows(); got[0] != first[0] {
		t.Fatalf("first row after pgup = %q, want %q", got[0], first[0])
	}

	// A taller window shows every key, so the list doesn't scroll.
	h.send(key("end"))
	h.send(tea.WindowSizeMsg{Width: 80, Height: 45})
	h.wantKeyList("Playlists tab", "ctrl+c/ctrl+q", "←/→ other tabs • esc close")
	h.dontWantView("↑/↓ scroll")
	h.send(key("down"))
	h.wantKeyList("Playlists tab", "ctrl+c/ctrl+q")

	// After a resize, the first ↑ moves at once, from the end it shows.
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(key("end"))
	h.send(tea.WindowSizeMsg{Width: 80, Height: 30})
	atEnd := h.keyListRows()
	h.send(key("up"))
	if got := h.keyListRows(); got[1] != atEnd[0] {
		t.Fatalf("second row after ↑ at the end of a taller window = %q, want %q", got[1], atEnd[0])
	}

	// Another tab shows its keys from the start.
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(key("end"), key("right"))
	if got := h.keyListRows(); !strings.Contains(got[0], "Podcasts tab") {
		t.Fatalf("first row on the Podcasts tab = %q, want its title", got[0])
	}
}

func TestKeyListShowsTheTabBesideTheOthersInWideWindows(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(key("?"))

	rows := h.keyListRows()
	if len(rows) == 0 || !strings.Contains(rows[0], "Playlists tab") || !strings.Contains(rows[0], "Playback") {
		t.Fatalf("rows = %q, want the keys of the tab beside the others", rows)
	}
	h.dontWantView("↑/↓ scroll")

	h.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	rows = h.keyListRows()
	if strings.Contains(rows[0], "Playback") {
		t.Fatalf("first row = %q, want the keys of the tab above the others", rows[0])
	}
	h.wantKeyList("Playback", "General")
}

func TestKeyListFitsEveryWindow(t *testing.T) {
	h := newHarness(t, 120, 40)
	h.send(key("?"))

	for _, width := range []int{10, 30, 53, 56, 57, 61, 80, 100, 120, 200} {
		for height := 1; height <= 45; height++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: height})
			for _, k := range []string{"home", "end"} {
				h.send(key(k))
				v := h.view()
				if got := lipgloss.Height(v); got > height {
					t.Fatalf("%dx%d after %s: view has %d lines:\n%s", width, height, k, got, v)
				}
				lines := strings.Split(v, "\n")
				for i, line := range lines {
					if got := lipgloss.Width(line); got > width {
						t.Fatalf("%dx%d after %s: line %d is %d columns wide:\n%s", width, height, k, i, got, v)
					}
				}
				fits := width >= lipgloss.Width(h.player().tabRow()) && h.player().contentDimension().height >= minContentHeight
				if fits && !strings.Contains(v, "╭─ "+keyListTitle) {
					t.Fatalf("%dx%d after %s: no list of keys:\n%s", width, height, k, v)
				}
				if fits && !strings.Contains(v, "esc close") {
					t.Fatalf("%dx%d after %s: no esc close:\n%s", width, height, k, v)
				}
			}
		}
	}
}

func TestTabsShowTheKeyListHint(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.wantView("tab songs • enter play • r refresh • ? keys")
	h.openTab(queueTab)
	h.wantView("enter play • r refresh • ? keys")
	h.openSearchTab()
	h.wantView(searchHelpText, "? keys")

	// The compact player has no room for the keys, so they follow ?.
	h.send(tea.WindowSizeMsg{Width: 120, Height: 15})
	h.wantView("? keys • space/k play/pause")
	h.openTab(playlistsTab)
	h.wantView("r refresh • ? keys • space/k play/pause")
}

func TestNarrowTabsKeepTheKeyListHint(t *testing.T) {
	h := newHarness(t, 120, 40)

	for _, tab := range []int{playlistsTab, podcastsTab, artistsTab, albumsTab, queueTab, searchTab} {
		h.openTab(tab)
		for width := 56; width <= 80; width++ {
			h.send(tea.WindowSizeMsg{Width: width, Height: 40})
			v := h.view()
			if strings.Contains(v, tooSmallWindow[:10]) {
				continue
			}
			if !strings.Contains(v, "? keys") {
				t.Fatalf("%s tab at %d columns: no ? keys:\n%s", h.player().tabs[tab].name, width, v)
			}
		}
		h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	}

	// The hints of the tab before ? make room for it, from the end.
	h.openTab(podcastsTab)
	h.send(tea.WindowSizeMsg{Width: 60, Height: 40})
	h.wantView("tab episodes • enter play • ? keys")
}

func TestKeyListHintHidesWhileTyping(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		h := newHarness(t, 120, 40)
		h.openSearchTab()
		h.wantView("? keys")

		h.send(key("/"))
		h.dontWantView("? keys")
		h.send(key("esc"))
		h.wantView("? keys")
	})

	t.Run("filter", func(t *testing.T) {
		h := newHarness(t, 120, 40)

		h.send(key("/"))
		h.dontWantView("? keys")
		h.send(key("esc"))
		h.wantView("? keys")
	})
}

func TestKeyListWrapsWhatKeysDoInNarrowWindows(t *testing.T) {
	h := newHarness(t, 57, 60)
	h.openTab(podcastsTab)
	h.send(key("?"))

	rows := h.keyListRows()
	for _, row := range rows {
		if strings.Contains(row, "…") {
			t.Fatalf("row %q is cut, want it to go on under itself:\n%s", row, h.view())
		}
	}
	h.wantKeyList("play the newest episode, or the", "selected one")
}
