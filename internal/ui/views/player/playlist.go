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
		leftFocus       bool
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
	statePlaylistInit = playlistState(iota)
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
		leftFocus:       true,
	}
}

func (p playlistModel) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.playlists)
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "tab":
			p.leftFocus = !p.leftFocus
		}

	case contentDimension:
		p.width = msg.width
		p.height = msg.height

	case error:
		p.state = statePlaylistError
		p.msg = fmt.Sprintf("Fail to load playlists:\n%s", msg.Error())
		return p, nil

	case []spotify.SimplePlaylist:
		p.state = statePlaylistLoaded

		var items []list.Item
		for _, p := range msg {
			title := p.Name
			if title == "" {
				title = string(p.ID)
			}
			items = append(items, item{title: title, desc: p.Description})
		}

		d := list.NewDefaultDelegate()
		d.Styles.SelectedTitle.
			Foreground(styles.LipglossAdaptiveColorGreenSpotify).
			BorderForeground(styles.LipglossAdaptiveColorGreenSpotify)
		d.ShowDescription = false
		l := list.New(items, d, 0, 0)
		l.FilterInput.CursorStyle = lipgloss.NewStyle().
			Foreground(styles.LipglossAdaptiveColorGreenSpotify)
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
		if p.leftFocus {
			list, cmd := p.list.Update(msg)
			p.list = &list

			p.alignHorizontal = lipgloss.Left
			return p, cmd
		}
	}

	return p, nil
}

func (p playlistModel) View() string {
	return lipgloss.JoinHorizontal(lipgloss.Top, p.leftView(), p.rightView())
}

func (p playlistModel) rightView() string {
	width := int(float64(p.width) * 0.8)
	if p.list != nil {
		i := p.list.SelectedItem().(item)
		return lipgloss.NewStyle().
			Width(width).
			AlignHorizontal(lipgloss.Center).
			Render(i.title)
	}
	return ""
}

func (p playlistModel) leftView() string {
	width := int(float64(p.width) * 0.2)
	if p.list != nil {
		p.list.SetSize(width, p.height-3)
		helpStyles := p.list.Help.Styles
		helpMsg := helpStyles.ShortKey.Inline(true).Render("/") + " " +
			helpStyles.ShortDesc.Inline(true).Render("filter")
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

func (p playlistModel) playlists() tea.Msg {
	playlists, err := p.spotifyClient.AllCurrentUsersPlaylists(
		p.ctx,
	)
	if err != nil {
		return err
	}
	return playlists
}
