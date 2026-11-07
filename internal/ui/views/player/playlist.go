package player

import (
	"fmt"

	"github.com/spot-go/spot-go/internal/utf8"

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

	playlistsLoaded []spotify.SimplePlaylist

	playlistsLoadFailed struct {
		err error
	}
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
			if !p.capturesInput() {
				p.leftFocus = !p.leftFocus
			}
		case "r":
			if p.state == statePlaylistError {
				p.state = statePlaylistInit
				p.msg = p.spinner.View()
				return p, p.Init()
			}
		}

	case contentDimension:
		p.width = msg.width
		p.height = msg.height

	case playlistsLoadFailed:
		p.state = statePlaylistError
		p.msg = fmt.Sprintf("Fail to load playlists:\n%s\n\nPress r to try again.", msg.err.Error())
		p.list = nil
		p.alignHorizontal = lipgloss.Center
		return p, nil

	case playlistsLoaded:
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
		p.alignHorizontal = lipgloss.Left

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
		if _, isKey := msg.(tea.KeyMsg); isKey && !p.leftFocus {
			return p, nil
		}
		list, cmd := p.list.Update(msg)
		p.list = &list
		return p, cmd
	}

	return p, nil
}

func (p playlistModel) capturesInput() bool {
	return p.list != nil && p.list.FilterState() == list.Filtering
}

func (p playlistModel) View() string {
	return lipgloss.JoinHorizontal(lipgloss.Top, p.leftView(), p.rightView())
}

func (p playlistModel) rightView() string {
	width := int(float64(p.width) * 0.75)
	if p.list != nil {
		i, ok := p.list.SelectedItem().(item)
		if !ok {
			return ""
		}
		title := utf8.Normalize(i.title)
		s := lipgloss.NewStyle().
			Width(width).
			AlignHorizontal(lipgloss.Center).
			Render(title)
		return s
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
		MaxHeight(p.height).
		Width(width).
		AlignHorizontal(p.alignHorizontal).
		Render(p.msg)
}

func (p playlistModel) playlists() tea.Msg {
	playlists, err := p.spotifyClient.AllCurrentUsersPlaylists(
		p.ctx,
	)
	if err != nil {
		return playlistsLoadFailed{err}
	}
	return playlistsLoaded(playlists)
}
