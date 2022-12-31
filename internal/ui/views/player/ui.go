package player

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	playerContext struct {
		ctx            context.Context
		spotifyClient  *libspotify.Client
		spotifyUser    *libspotify.PrivateUser
		spotifydPlayer *libspotify.PlayerDevice
	}

	Model struct {
		playerContext
		views.WindowSize

		tabs          []string
		tabContent    []content
		activeContent tea.Model
		activeTab     int
		initialized   bool
	}

	content interface {
		tea.Model
		SetDimension(width, height int) content
	}
)

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┴", "─", "┴")
	docStyle          = lipgloss.NewStyle()
	highlightColor    = lipgloss.AdaptiveColor{
		Light: styles.ColorGreenSpotify,
		Dark:  styles.ColorGreenSpotify,
	}
	inactiveTabStyle = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(highlightColor).
				Padding(0, 1)
	activeTabStyle = inactiveTabStyle.Copy().Border(activeTabBorder, true)
	windowStyle    = lipgloss.NewStyle().
			BorderForeground(highlightColor).
			Border(lipgloss.NormalBorder()).
			UnsetBorderTop()
)

func NewModel(
	windowSize views.WindowSize,
	ctx context.Context,
	spotifyClient *libspotify.Client,
	spotifyUser *libspotify.PrivateUser,
	spotifydPlayer *libspotify.PlayerDevice,
) Model {
	playerContext := playerContext{
		ctx:            ctx,
		spotifyClient:  spotifyClient,
		spotifyUser:    spotifyUser,
		spotifydPlayer: spotifydPlayer,
	}
	tabs := []string{"Search", "Playlists", "Podcasts", "Artists", "Albums"}
	tabContent := []content{
		playlistModel{},
		newPlaylistModel(playerContext),
		playlistModel{},
		playlistModel{},
		playlistModel{},
	}
	return Model{
		playerContext: playerContext,
		tabs:          tabs,
		tabContent:    tabContent,
		activeTab:     1,
		WindowSize:    windowSize,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	activeContent := m.tabContent[m.activeTab]
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right":
			m.activeTab = min(m.activeTab+1, len(m.tabs)-1)
			return m, activeContent.Init()
		case "left":
			m.activeTab = max(m.activeTab-1, 0)
			return m, activeContent.Init()
		}
		var cmd tea.Cmd
		m.activeContent, cmd = m.activeContent.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg

		return m, nil
	}

	if !m.initialized {
		m.initialized = true
		m.activeContent = activeContent
		return m, m.activeContent.Init()
	}

	var cmd tea.Cmd
	m.activeContent, cmd = m.activeContent.Update(msg)
	return m, cmd
}

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

func (m Model) View() string {
	doc := strings.Builder{}

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
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	tabGap := inactiveTabStyle.Copy().
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false)
	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	gap := tabGap.Render(strings.Repeat(" ", max(0, m.Width-lipgloss.Width(row)-5)))
	closeBox := lipgloss.NewStyle().
		Foreground(styles.LipglossColorForegroundGreenSpotify).
		Render("┐")
	row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap, closeBox)
	doc.WriteString(row)
	doc.WriteString("\n")

	contentWidth := lipgloss.Width(row) - windowStyle.GetHorizontalFrameSize()
	contentHeight := m.Height - lipgloss.Height(row) - 2

	activeContent := m.tabContent[m.activeTab]
	if m.activeContent != nil {
		activeContent = m.activeContent.(content)
	}
	activeContent = activeContent.SetDimension(contentWidth, contentHeight)

	content := windowStyle.
		Width(contentWidth).
		Height(contentHeight).
		Render(activeContent.View())
	doc.WriteString(content)

	return docStyle.Height(m.Height).Render(doc.String())
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
