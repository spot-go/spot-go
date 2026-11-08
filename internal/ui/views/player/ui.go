package player

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	}

	Model struct {
		playerContext
		views.WindowSize

		tabs           []tab
		activeTab      int
		playbackErrors <-chan error
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

	spotifydPlaybackFailed struct {
		err error
	}

	inputCapturer interface {
		capturesInput() bool
	}
)

const (
	playlistsTab     = 1
	minContentHeight = 4
	tooSmallWindow   = "The terminal window is too small. Please make it bigger."
)

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┴", "─", "┴")
	docStyle          = lipgloss.NewStyle()
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(styles.LipglossAdaptiveColorGreenSpotify).
				Padding(0, 1)
	activeTabStyle = inactiveTabStyle.Copy().Border(activeTabBorder, true)
	windowStyle    = lipgloss.NewStyle().
			BorderForeground(styles.LipglossAdaptiveColorGreenSpotify).
			Border(lipgloss.NormalBorder()).
			UnsetBorderTop()
)

func NewModel(
	windowSize views.WindowSize,
	ctx context.Context,
	spotifyClient *spotify.Client,
	spotifyUser *libspotify.PrivateUser,
	playerDevice *libspotify.PlayerDevice,
	playbackErrors <-chan error,
) Model {
	playerContext := playerContext{
		ctx:           ctx,
		spotifyClient: spotifyClient,
		spotifyUser:   spotifyUser,
		playerDevice:  playerDevice,
	}
	m := Model{
		playerContext: playerContext,
		WindowSize:    windowSize,
		tabs: []tab{
			placeholderTab("Search"),
			{name: "Playlists", content: newPlaylistModel(playerContext), initialized: true},
			placeholderTab("Podcasts"),
			placeholderTab("Artists"),
			placeholderTab("Albums"),
		},
		activeTab:      playlistsTab,
		playbackErrors: playbackErrors,
	}
	m.broadcast(m.contentDimension())
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.tabs[m.activeTab].content.Init(), m.waitForPlaybackError())
}

func (m Model) waitForPlaybackError() tea.Cmd {
	if m.playbackErrors == nil {
		return nil
	}
	return func() tea.Msg {
		err, ok := <-m.playbackErrors
		if !ok {
			return nil
		}
		return spotifydPlaybackFailed{err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		}
		return m, m.updateTab(m.activeTab, msg)

	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		return m, m.broadcast(m.contentDimension())

	case spotifydPlaybackFailed:
		return m, tea.Batch(m.broadcast(msg), m.waitForPlaybackError())
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
	return cmd
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
			style = activeTabStyle.Copy().
				Background(styles.LipglossColorBackgroundGreenSpotify).
				Foreground(styles.LipglossColorBlack)
		} else {
			style = inactiveTabStyle.Copy()
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

	tabGap := inactiveTabStyle.Copy().
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

func (m Model) View() string {
	row, cd := m.layout()
	if lipgloss.Width(row) > m.Width || cd.height < minContentHeight {
		return lipgloss.NewStyle().
			Width(m.Width).
			MaxHeight(m.Height).
			Render(tooSmallWindow)
	}

	content := lipgloss.NewStyle().
		MaxWidth(cd.width).
		MaxHeight(cd.height).
		Render(m.tabs[m.activeTab].content.View())
	window := windowStyle.
		Width(cd.width).
		Height(cd.height).
		Render(content)
	return docStyle.Height(m.Height).Render(row + "\n" + window)
}

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
