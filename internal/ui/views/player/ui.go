package player

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	playerContext struct {
		ctx           context.Context
		spotifyClient *spotify.Client
		spotifyUser   *libspotify.PrivateUser
		playerDevice  *libspotify.PlayerDevice
		historyStore  *library.Store
		history       *library.History
		plays         *int
	}

	Model struct {
		playerContext
		views.WindowSize

		tabs          []tab
		activeTab     int
		toggle        playbackToggle
		volume        volumeControl
		modes         [modeCount]modeControl
		playerBar     playerBar
		volumeSaver   *volumeSaver
		confirmEvery  time.Duration
		startPolls    int
		progressEvery time.Duration
		ticking       bool
		after         func(time.Duration, tea.Msg) tea.Cmd
		publish       func(media.Info)
		nowPlaying    nowPlaying
	}

	tab struct {
		name        string
		content     tea.Model
		initialized bool
	}

	contentDimension struct {
		width  int
		height int
		// spotifyKeys is true when the tab shows the keys that control
		// Spotify, as the compact player has no room for them.
		spotifyKeys bool
	}

	inputCapturer interface {
		capturesInput() bool
	}

	tabFilterMatches struct {
		tab     int
		matches list.FilterMatchesMsg
	}
)

const (
	searchTab        = 0
	queueTab         = 1
	playlistsTab     = 2
	podcastsTab      = 3
	artistsTab       = 4
	albumsTab        = 5
	minContentHeight = 4
	tabPadding       = 1
	tooSmallWindow   = "The terminal window is too small. Please make it bigger."
)

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┴", "─", "┴")
	docStyle          = lipgloss.NewStyle()
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(styles.LipglossColorForegroundGreenSpotify).
				Padding(0, tabPadding)
	activeTabStyle = inactiveTabStyle.Border(activeTabBorder, true)
	// windowStyle frames the tab content on its sides. The tab row is
	// above it, and the player below.
	windowStyle = lipgloss.NewStyle().
			BorderForeground(styles.LipglossColorForegroundGreenSpotify).
			Border(lipgloss.NormalBorder(), false, true)
)

func NewModel(
	windowSize views.WindowSize,
	ctx context.Context,
	spotifyClient *spotify.Client,
	spotifyUser *libspotify.PrivateUser,
	playerDevice *libspotify.PlayerDevice,
	historyStore *library.Store,
	publish func(media.Info),
	saveVolume func(int) error,
) Model {
	var (
		history    library.History
		historyErr error
	)
	if historyStore != nil {
		history, historyErr = historyStore.Load()
		if historyErr != nil && !errors.Is(historyErr, library.ErrCorrupt) {
			historyStore = nil
		}
	}
	playerContext := playerContext{
		ctx:           ctx,
		spotifyClient: spotifyClient,
		spotifyUser:   spotifyUser,
		playerDevice:  playerDevice,
		historyStore:  historyStore,
		history:       &history,
		plays:         new(int),
	}
	m := Model{
		playerContext: playerContext,
		WindowSize:    windowSize,
		tabs: []tab{
			{name: "Search", content: newSearchModel(playerContext), initialized: true},
			{name: "Queue", content: newQueueModel(playerContext)},
			// The Playlists tab loads at the start even though spot-go opens
			// on Search: it follows what Spotify plays, for every tab.
			{name: "Playlists", content: newPlaylistModel(playerContext, historyErr), initialized: true},
			{name: "Podcasts", content: newPodcastModel(playerContext, historyErr)},
			{name: "Artists", content: newArtistModel(playerContext, historyErr)},
			{name: "Albums", content: newAlbumModel(playerContext, historyErr)},
		},
		activeTab:     searchTab,
		volumeSaver:   newVolumeSaver(saveVolume),
		confirmEvery:  confirmPollEvery,
		startPolls:    confirmPolls,
		progressEvery: progressInterval,
		after:         after,
		publish:       publish,
	}
	m.broadcast(m.contentDimension())
	return m
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		if t.initialized {
			cmds = append(cmds, t.content.Init())
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(progressTicked); ok {
		m.ticking = false
	}
	m, cmd := m.update(msg)
	return m.tickProgress(cmd)
}

func (m Model) tickProgress(cmd tea.Cmd) (Model, tea.Cmd) {
	if m.ticking || m.progressEvery <= 0 {
		return m, cmd
	}
	shower, ok := m.tabs[m.activeTab].content.(playbackShower)
	if !ok {
		return m, cmd
	}
	delay, ok := shower.status().nextTick(time.Now(), m.progressEvery)
	if !ok {
		return m, cmd
	}
	m.ticking = true
	return m, tea.Batch(cmd, m.after(delay, progressTicked{}))
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressTicked:
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+q", "ctrl+c":
			return m, tea.Quit
		case "left":
			if !m.activeCapturesInput() {
				return m.switchTab(m.activeTab - 1)
			}
		case "right":
			if !m.activeCapturesInput() {
				return m.switchTab(m.activeTab + 1)
			}
		case "k", "space":
			if !m.activeCapturesInput() {
				return m.changePlayback(toggleChange)
			}
		case "n":
			if !m.activeCapturesInput() {
				return m.changePlayback(nextChange)
			}
		case "p":
			if !m.activeCapturesInput() {
				return m.changePlayback(previousChange)
			}
		case ">", ".":
			if !m.activeCapturesInput() {
				return m.seek(seekStep)
			}
		case "<", ",":
			if !m.activeCapturesInput() {
				return m.seek(-seekStep)
			}
		case "ctrl+s":
			if !m.activeCapturesInput() {
				return m.changeMode(shuffleMode, 1)
			}
		case "ctrl+r":
			if !m.activeCapturesInput() {
				return m.changeMode(repeatMode, 1)
			}
		case "+", "=":
			if !m.activeCapturesInput() {
				return m.changeVolume(volumeStep)
			}
		case "-":
			if !m.activeCapturesInput() {
				return m.changeVolume(-volumeStep)
			}
		}
		return m, m.updateTab(m.activeTab, msg)

	case media.Command:
		if change, ok := mediaChanges[msg]; ok {
			return m.changePlayback(change)
		}
		return m, nil

	case playbackToggled:
		m = m.playbackToggled(msg)
		if msg.seek && msg.err != nil {
			// What plays didn't change, so the tabs keep showing it.
			return m.notify("Fail to seek: "+errorText(msg.err), true)
		}
		if msg.err == nil && msg.plays == *m.plays {
			m = m.publishToggled(msg)
			m.playerBar.idle = false
		}
		broadcast := m.broadcast(msg)
		m, skip := m.nextSkip()
		return m, tea.Batch(broadcast, skip)

	case volumeChanged:
		return m.volumeChanged(msg)

	case modeChanged:
		return m.modeChanged(msg)

	case noticeHidden:
		return m.noticeHidden(msg), nil

	case playbackStarted:
		return m.playbackStarted(msg, msg.uri, msg.item, msg.info())

	case episodePlaybackStarted:
		return m.playbackStarted(msg, msg.show, msg.episode, msg.info())

	case albumPlaybackStarted:
		return m.playbackStarted(msg, msg.uri, msg.item, msg.info())

	case artistPlaybackStarted:
		return m.playbackStarted(msg, msg.uri, "", msg.info())

	case searchPlaybackStarted:
		return m.playbackStarted(msg, msg.uri, msg.item, msg.info())

	case queuePlaybackStarted:
		return m.skippedTo(msg)

	case playbackConfirmed:
		return m.playbackConfirmed(msg)

	case playbackFailed, episodePlaybackFailed, albumPlaybackFailed, artistPlaybackFailed, searchPlaybackFailed, queuePlaybackFailed:
		m.toggle.settled++
		return m, m.broadcast(msg)

	case playbackPolled:
		if msg.err == nil && msg.plays == *m.plays {
			m = m.publishPolled(msg.playing)
			m = m.followPolled(msg.playing)
		}
		return m, tea.Batch(m.broadcast(msg), m.rememberPolledVolume(msg))

	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		return m, m.broadcast(m.contentDimension())

	case tabFilterMatches:
		if msg.tab < 0 || msg.tab >= len(m.tabs) {
			return m, nil
		}
		return m, m.updateTab(msg.tab, msg.matches)
	}

	return m, m.broadcast(msg)
}

func (m Model) switchTab(i int) (Model, tea.Cmd) {
	i = max(0, min(i, len(m.tabs)-1))
	if i == m.activeTab {
		return m, nil
	}

	hidden := m.updateTab(m.activeTab, tabHidden{})
	m.activeTab = i
	var init tea.Cmd
	if !m.tabs[i].initialized {
		m.tabs[i].initialized = true
		init = m.tabs[i].content.Init()
	}
	return m, tea.Batch(hidden, init, m.updateTab(i, tabShown{}))
}

func (m Model) updateTab(i int, msg tea.Msg) tea.Cmd {
	content, cmd := m.tabs[i].content.Update(msg)
	m.tabs[i].content = content
	return ownFilterMatches(i, cmd)
}

func ownFilterMatches(tab int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case list.FilterMatchesMsg:
			return tabFilterMatches{tab: tab, matches: msg}
		case tea.BatchMsg:
			owned := make(tea.BatchMsg, len(msg))
			for i, c := range msg {
				owned[i] = ownFilterMatches(tab, c)
			}
			return owned
		default:
			return msg
		}
	}
}

func (m Model) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.tabs))
	for i := range m.tabs {
		cmds[i] = m.updateTab(i, msg)
	}
	return tea.Batch(cmds...)
}

func (m Model) activeCapturesInput() bool {
	c, ok := m.tabs[m.activeTab].content.(inputCapturer)
	return ok && c.capturesInput()
}

// tabRow is the row of tabs. When it doesn't fit the window, the tabs drop
// the space around their names.
func (m Model) tabRow() string {
	row := m.tabRowWith(tabPadding)
	if lipgloss.Width(row) > m.Width {
		row = m.tabRowWith(0)
	}
	return row
}

func (m Model) tabRowWith(padding int) string {
	var renderedTabs []string
	for i, t := range m.tabs {
		var style lipgloss.Style
		isFirst, isActive := i == 0, i == m.activeTab
		if isActive {
			style = activeTabStyle.
				Background(styles.LipglossColorBackgroundGreenSpotify).
				Foreground(styles.LipglossColorBlack)
		} else {
			style = inactiveTabStyle
		}
		style = style.Padding(0, padding)
		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "│"
		} else if isFirst && !isActive {
			border.BottomLeft = "├"
		}
		style = style.Border(border)
		renderedTabs = append(renderedTabs, style.Render(t.name))
	}

	tabGap := inactiveTabStyle.
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false)
	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	closeBox := lipgloss.NewStyle().
		Foreground(styles.LipglossColorForegroundGreenSpotify).
		Render("┐")
	space := m.Width - lipgloss.Width(row) - lipgloss.Width(tabGap.Render("")) - lipgloss.Width(closeBox)
	gap := tabGap.Render(strings.Repeat(" ", max(0, space)))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap, closeBox)
}

// layout is the tab row, the size of the tab content under it, and whether
// the window has room for the full player under the content.
func (m Model) layout() (string, contentDimension, bool) {
	row := m.tabRow()
	full := m.hasRoomForFullPlayer(lipgloss.Height(row))
	chrome := compactPlayerChrome
	if full {
		chrome = fullPlayerChrome
	}
	return row, contentDimension{
		width:       lipgloss.Width(row) - windowStyle.GetHorizontalFrameSize(),
		height:      m.Height - lipgloss.Height(row) - chrome,
		spotifyKeys: !full,
	}, full
}

func (m Model) contentDimension() contentDimension {
	_, cd, _ := m.layout()
	return cd
}

func (m Model) View() tea.View {
	row, cd, full := m.layout()
	if lipgloss.Width(row) > m.Width || cd.height < minContentHeight {
		return views.FullScreen(lipgloss.NewStyle().
			Width(m.Width).
			MaxHeight(m.Height).
			Render(tooSmallWindow), m.Height)
	}

	content := lipgloss.NewStyle().
		MaxWidth(cd.width).
		MaxHeight(cd.height).
		Render(m.tabs[m.activeTab].content.View().Content)
	window := windowStyle.
		Width(cd.width + windowStyle.GetHorizontalBorderSize()).
		Height(cd.height).
		Render(content)
	player := m.playerView(lipgloss.Width(row), full, time.Now())
	return views.FullScreen(docStyle.Height(m.Height).Render(row+"\n"+window+"\n"+player), m.Height)
}

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}
