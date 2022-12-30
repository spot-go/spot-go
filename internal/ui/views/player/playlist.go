package player

import (
	"github.com/charmbracelet/bubbles/spinner"
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
	context
	spinner spinner.Model
	width   int
	height  int
}

func newPlaylistModel(ctx context) playlistModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return playlistModel{
		context: ctx,
		spinner: s,
	}
}

func (p playlistModel) Init() tea.Cmd {
	return p.spinner.Tick
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.spinner, cmd = p.spinner.Update(msg)
	return p, cmd
}

func (p playlistModel) View() string {
	return borderStyle.
		Height(p.height).
		Width(int(float64(p.width)*0.2)).
		Align(lipgloss.Center, lipgloss.Center).
		Render(p.spinner.View())
}

func (p playlistModel) SetDimension(width, height int) content {
	p.width = width
	p.height = height
	return p
}
