package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/views"
	libspotify "github.com/zmb3/spotify/v2"
)

var testPlaylists = []string{"Rock", "Jazz", "Samba", "Blues"}

type fakeAPI struct{}

func (fakeAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	items := make([]string, len(testPlaylists))
	for i, name := range testPlaylists {
		items[i] = fmt.Sprintf(`{"id":"p%d","name":%q}`, i, name)
	}
	body := fmt.Sprintf(`{"items":[%s],"total":%d,"limit":50,"offset":0}`, strings.Join(items, ","), len(items))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

type harness struct {
	t             *testing.T
	model         tea.Model
	playlistLoads int
}

func newModel(width, height int) Model {
	client := spotify.NewClient(&http.Client{Transport: fakeAPI{}})
	size := views.WindowSize{WindowSizeMsg: tea.WindowSizeMsg{Width: width, Height: height}}
	return NewModel(size, context.Background(), client, &libspotify.PrivateUser{}, &libspotify.PlayerDevice{})
}

func newHarness(t *testing.T, width, height int) *harness {
	t.Helper()

	m := newModel(width, height)
	h := &harness{t: t, model: m}
	h.run(m.Init())
	return h
}

func (h *harness) send(msgs ...tea.Msg) {
	h.t.Helper()

	queue := msgs
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			h.t.Fatal("too many messages, the model is probably in an update loop")
		}
		msg := queue[0]
		queue = queue[1:]
		if _, ok := msg.(spinner.TickMsg); ok {
			continue
		}
		if _, ok := msg.(playlistsLoaded); ok {
			h.playlistLoads++
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
	return h.model.View()
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
	case placeholderModel:
		return contentDimension{width: c.width, height: c.height}
	}
	return contentDimension{}
}

type tallModel struct{}

func (tallModel) Init() tea.Cmd                         { return nil }
func (t tallModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }
func (tallModel) View() string                          { return strings.Repeat(strings.Repeat("x", 300)+"\n", 100) }

func key(k string) tea.Msg {
	switch k {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+q":
		return tea.KeyMsg{Type: tea.KeyCtrlQ}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
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
}

func TestTabContentFollowsActiveTab(t *testing.T) {
	h := newHarness(t, 120, 40)

	h.send(key("right"))
	if v := h.view(); !strings.Contains(v, "Podcasts is coming soon") || strings.Contains(v, "Rock") {
		t.Fatalf("Podcasts tab shows the wrong content:\n%s", v)
	}

	h.send(key("down"))
	h.send(key("left"))
	if v := h.view(); !strings.Contains(v, "Rock") {
		t.Fatalf("Playlists tab does not show the playlists:\n%s", v)
	}
	if got := h.selected(); got != "Rock" {
		t.Fatalf("keys on the Podcasts tab moved the playlist cursor to %q", got)
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
	if got := h.player().activeTab; got != playlistsTab+1 {
		t.Fatalf("activeTab after leaving the filter = %d, want %d", got, playlistsTab+1)
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
	if got := h.player().activeTab; got != playlistsTab+1 {
		t.Fatalf("activeTab = %d, want %d", got, playlistsTab+1)
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

	if v := model.View(); !strings.Contains(v, tooSmallWindow) {
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
	h.player().tabs[0].content = tallModel{}

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
