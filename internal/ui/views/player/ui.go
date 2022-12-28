package player

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views"
)

type Model struct {
	views.WindowSize
	tabs       []string
	tabContent []string
	activeTab  int
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	docStyle          = lipgloss.NewStyle().Padding(1, 2, 1, 2)
	highlightColor    = lipgloss.AdaptiveColor{
		Light: styles.ColorGreenSpotify,
		Dark:  styles.ColorGreenSpotify,
	}
	inactiveTabStyle = lipgloss.NewStyle().Border(inactiveTabBorder, true).BorderForeground(highlightColor).Padding(0, 1)
	activeTabStyle   = inactiveTabStyle.Copy().Border(activeTabBorder, true)
	windowStyle      = lipgloss.NewStyle().
				BorderForeground(highlightColor).
				Padding(2, 0).
				Align(lipgloss.Center).
				Border(lipgloss.NormalBorder()).
				UnsetBorderTop()
)

func NewModel(windowSize views.WindowSize) Model {
	tabs := []string{"Search", "Playlists", "Podcasts", "Artists", "Albums"}
	tabContent := []string{"Lip Gloss Tab", "Blush Tab", "Eye Shadow Tab", "Mascara Tab", "Foundation Tab"}
	return Model{
		tabs:       tabs,
		tabContent: tabContent,
		WindowSize: windowSize,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right", "tab":
			m.activeTab = min(m.activeTab+1, len(m.tabs)-1)
			return m, nil
		case "left", "shift+tab":
			m.activeTab = max(m.activeTab-1, 0)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg

		return m, nil
	}

	return m, nil
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
	gap := tabGap.Render(strings.Repeat(" ", m.Width-lipgloss.Width(row)-5))
	closeBox := lipgloss.NewStyle().
		Foreground(styles.LipglossColorForegroundGreenSpotify).
		Render("┐")
	row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap, closeBox)
	doc.WriteString(row)
	doc.WriteString("\n")

	contentWidth := lipgloss.Width(row) - windowStyle.GetHorizontalFrameSize()
	contentHeight := m.Height - lipgloss.Height(row) - 2
	content := windowStyle.
		Width(contentWidth).
		Height(contentHeight).
		Render(m.tabContent[m.activeTab])
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
