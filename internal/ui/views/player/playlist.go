package player

import (
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

var (
	border = lipgloss.Border{
		Right: "│",
	}
	borderStyle = lipgloss.NewStyle().
			Border(border).
			BorderForeground(styles.LipglossColorForegroundGreenSpotify).
			UnsetBorderTop().
			UnsetBorderBottom()
)

type playlistModel struct {
	width  int
	height int
}

func (p playlistModel) Init() tea.Cmd {
	return nil
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return p, nil
}

func (p playlistModel) View() string {
	return borderStyle.
		Height(p.height).
		Width(int(math.Round(float64(p.width) * 0.1))).
		AlignHorizontal(lipgloss.Center).
		Render("border")
}

func (p *playlistModel) SetDimension(width, height int) {
	p.width = width
	p.height = height
}
