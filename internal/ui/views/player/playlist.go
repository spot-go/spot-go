package player

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
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
		spinner         spinner.Model
		width           int
		height          int
		alignHorizontal lipgloss.Position
		msg             string
		state           playlistState
		list            *list.Model
	}

	item struct {
		title, desc string
	}

	playlistState int
)

const (
	statePlaylistInit = iota
	statePlaylistError
	statePlaylistLoaded
)

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

func newPlaylistModel(ctx playerContext) playlistModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return playlistModel{
		playerContext:   ctx,
		spinner:         s,
		alignHorizontal: lipgloss.Center,
	}
}

func (p playlistModel) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.playlists)
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case error:
		p.state = statePlaylistError
		p.msg = fmt.Sprintf("Fail to load playlists:\n%s", msg.Error())
		return p, nil

	case *spotify.SimplePlaylistPage:
		p.state = statePlaylistLoaded

		var items []list.Item
		for _, p := range msg.Playlists {
			items = append(items, item{title: p.Name, desc: p.Description})
		}

		d := list.NewDefaultDelegate()
		d.Styles.SelectedTitle.Foreground(highlightColor).BorderForeground(highlightColor)
		d.ShowDescription = false
		l := list.New(items, d, 0, 0)
		l.FilterInput.CursorStyle = lipgloss.NewStyle().
			Foreground(highlightColor)
		l.SetStatusBarItemName("playlist", "playlists")
		l.SetShowTitle(false)
		l.SetShowHelp(false)
		l.DisableQuitKeybindings()

		p.list = &l

		return p, nil
	}

	var cmd tea.Cmd
	switch p.state {
	case statePlaylistInit:
		p.spinner, cmd = p.spinner.Update(msg)
		p.msg = p.spinner.View()
		return p, cmd

	case statePlaylistLoaded:
		list, cmd := p.list.Update(msg)
		p.list = &list

		p.alignHorizontal = lipgloss.Left
		return p, cmd
	}

	return p, nil
}

func (p playlistModel) View() string {
	width := int(float64(p.width) * 0.2)
	if p.list != nil {
		p.list.SetSize(width, p.height-2)
		helpMsg := p.list.Help.Styles.ShortKey.Inline(true).Render("/") + " " +
			p.list.Help.Styles.ShortDesc.Inline(true).Render("filter")
		p.msg = p.list.View() + "\n\n" +
			lipgloss.NewStyle().
				PaddingLeft(6).
				Render(helpMsg)
	}
	return borderStyle.
		Height(p.height).
		Width(width).
		AlignHorizontal(p.alignHorizontal).
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
