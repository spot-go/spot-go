package player

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/zmb3/spotify/v2"
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

type (
	playlistModel struct {
		playerContext
		spinner spinner.Model
		width   int
		height  int
		msg     string
		state   playlistState
	}

	playlistState int
)

const (
	statePlaylistInit = iota
	statePlaylistError
	statePlaylistLoaded
)

func newPlaylistModel(ctx playerContext) playlistModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return playlistModel{
		playerContext: ctx,
		spinner:       s,
	}
}

func (p playlistModel) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.playlists)
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.state != statePlaylistInit {
		return p, nil
	}

	switch msg := msg.(type) {
	case error:
		p.msg = fmt.Sprintf("Fail to load playlists:\n%s", msg.Error())
		p.state = statePlaylistError
		return p, nil
	case *spotify.SimplePlaylistPage:
		p.state = statePlaylistLoaded
		return p, nil
	}

	var cmd tea.Cmd
	p.spinner, cmd = p.spinner.Update(msg)
	p.msg = p.spinner.View()
	return p, cmd
}

func (p playlistModel) View() string {
	return borderStyle.
		Height(p.height).
		Width(int(float64(p.width)*0.2)).
		Align(lipgloss.Center, lipgloss.Center).
		Render(p.msg)
}

func (p playlistModel) SetDimension(width, height int) content {
	p.width = width
	p.height = height
	return p
}

func (p playlistModel) playlists() tea.Msg {
	playlists, err := p.spotifyClient.CurrentUsersPlaylists(
		p.ctx,
	)
	if err != nil {
		return err
	}
	return playlists
}
