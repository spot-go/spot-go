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

		tabs       []tab
		activeTab  int
		toggle     playbackToggle
		skipEvery  time.Duration
		publish    func(media.Info)
		nowPlaying nowPlaying
	}

	tab struct {
		name        string
		content     tea.Model
		initialized bool
	}

	contentDimension struct {
		width  int
		height int
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
	playlistsTab     = 1
	podcastsTab      = 2
	minContentHeight = 4
	tooSmallWindow   = "The terminal window is too small. Please make it bigger."
)

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┴", "─", "┴")
	docStyle          = lipgloss.NewStyle()
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(styles.LipglossColorForegroundGreenSpotify).
				Padding(0, 1)
	activeTabStyle = inactiveTabStyle.Border(activeTabBorder, true)
	windowStyle    = lipgloss.NewStyle().
			BorderForeground(styles.LipglossColorForegroundGreenSpotify).
			Border(lipgloss.NormalBorder()).
			UnsetBorderTop()
)

func NewModel(
	windowSize views.WindowSize,
	ctx context.Context,
	spotifyClient *spotify.Client,
	spotifyUser *libspotify.PrivateUser,
	playerDevice *libspotify.PlayerDevice,
	historyStore *library.Store,
	publish func(media.Info),
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
			placeholderTab("Search"),
			{name: "Playlists", content: newPlaylistModel(playerContext, historyErr), initialized: true},
			{name: "Podcasts", content: newPodcastModel(playerContext, historyErr)},
			placeholderTab("Artists"),
			placeholderTab("Albums"),
		},
		activeTab: playlistsTab,
		skipEvery: skipPollEvery,
		publish:   publish,
	}
	m.broadcast(m.contentDimension())
	return m
}

func (m Model) Init() tea.Cmd {
	return m.tabs[m.activeTab].content.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		}
		return m, m.updateTab(m.activeTab, msg)

	case media.Command:
		if change, ok := mediaChanges[msg]; ok {
			return m.changePlayback(change)
		}
		return m, nil

	case playbackToggled:
		m = m.playbackToggled(msg)
		if msg.err == nil && msg.plays == *m.plays {
			m = m.publishToggled(msg)
		}
		broadcast := m.broadcast(msg)
		m, skip := m.nextSkip()
		return m, tea.Batch(broadcast, skip)

	case playbackStarted:
		m.toggle.settled++
		m = m.publishCommand(msg.item, media.Info{Title: msg.name, Playing: true})
		return m, m.broadcast(msg)

	case episodePlaybackStarted:
		m.toggle.settled++
		m = m.publishCommand(msg.episode, media.Info{Title: msg.name, Elapsed: msg.from, Playing: true})
		return m, m.broadcast(msg)

	case playbackFailed, episodePlaybackFailed:
		m.toggle.settled++
		return m, m.broadcast(msg)

	case playbackPolled:
		if msg.err == nil && msg.plays == *m.plays {
			m = m.publishPolled(msg.playing)
		}
		return m, m.broadcast(msg)

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

func (m Model) switchTab(i int) (tea.Model, tea.Cmd) {
	i = max(0, min(i, len(m.tabs)-1))
	if i == m.activeTab {
		return m, nil
	}

	m.activeTab = i
	if m.tabs[i].initialized {
		return m, nil
	}
	m.tabs[i].initialized = true
	return m, m.tabs[i].content.Init()
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

func (m Model) tabRow() string {
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

func (m Model) layout() (string, contentDimension) {
	row := m.tabRow()
	return row, contentDimension{
		width:  lipgloss.Width(row) - windowStyle.GetHorizontalFrameSize(),
		height: m.Height - lipgloss.Height(row) - 2,
	}
}

func (m Model) contentDimension() contentDimension {
	_, cd := m.layout()
	return cd
}

func (m Model) View() tea.View {
	row, cd := m.layout()
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
		Height(cd.height + windowStyle.GetVerticalBorderSize()).
		Render(content)
	return views.FullScreen(docStyle.Height(m.Height).Render(row+"\n"+window), m.Height)
}

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}
