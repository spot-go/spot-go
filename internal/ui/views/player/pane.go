package player

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

type paneSize struct {
	width       int
	height      int
	spotifyKeys bool
}

const (
	rightViewChrome     = 4
	rightViewPadding    = 1
	minListStatusHeight = 4
	leftHelpIndent      = 6
	followGrace         = 3 * time.Second
)

var (
	border = lipgloss.Border{
		Left:  " ",
		Right: "│",
	}
	borderStyle = lipgloss.NewStyle().
			Border(border).
			BorderForeground(styles.LipglossColorForegroundGreenSpotify).
			UnsetBorderTop().
			UnsetBorderBottom()
	playingStyle = lipgloss.NewStyle().
			Foreground(styles.LipglossColorForegroundGreenSpotify)
	playbackErrorStyle = lipgloss.NewStyle().
				Foreground(styles.LipglossColorRed)
	paneTitleStyle = lipgloss.NewStyle().
			Background(styles.LipglossColorBackgroundGreenSpotify).
			Foreground(styles.LipglossColorBlack).
			Padding(0, 1)
)

func after(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return msg
	})
}

func errorText(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return err.Error()
}

func newList(items []list.Item, delegate list.ItemDelegate) list.Model {
	l := list.New(items, delegate, 0, 0)
	l.Styles = list.DefaultStyles(styles.HasDarkBackground)
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	l.FilterInput.SetStyles(l.Styles.Filter)
	l.Help.Styles = help.DefaultStyles(styles.HasDarkBackground)
	return l
}

func newLibraryList(items []list.Item, title, singular, plural string) list.Model {
	d := list.NewDefaultDelegate()
	d.Styles = list.NewDefaultItemStyles(styles.HasDarkBackground)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(styles.LipglossColorForegroundGreenSpotify).
		BorderForeground(styles.LipglossColorForegroundGreenSpotify)
	d.ShowDescription = false
	l := newList(items, d)
	filterStyles := l.Styles.Filter
	filterStyles.Cursor.Color = styles.LipglossColorForegroundGreenSpotify
	l.FilterInput.SetStyles(filterStyles)
	l.Filter = list.UnsortedFilter
	l.Title = title
	l.Styles.Title = l.Styles.Title.
		Background(styles.LipglossColorBackgroundGreenSpotify).
		Foreground(styles.LipglossColorBlack)
	l.Styles.TitleBar = l.Styles.TitleBar.PaddingLeft(1)
	l.SetStatusBarItemName(singular, plural)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	return l
}

func (s paneSize) newRightList(items []list.Item, delegate list.ItemDelegate, singular, plural string) *list.Model {
	l := newList(items, delegate)
	l.SetFilteringEnabled(false)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	l.SetStatusBarItemName(singular, plural)
	s.resizeRightList(&l)
	return &l
}

func (s paneSize) resizeRightList(l *list.Model) {
	l.SetShowStatusBar(s.listHeight() >= minListStatusHeight)
	l.SetSize(s.rightWidth(), s.listHeight())
}

func (s paneSize) leftWidth() int {
	return int(float64(s.width) * 0.2)
}

func (s paneSize) rightWidth() int {
	return max(0, int(float64(s.width)*0.75)-rightViewPadding)
}

func (s paneSize) listHeight() int {
	return max(0, s.height-rightViewChrome)
}

func (s paneSize) leftPane(l *list.Model, msg string, align lipgloss.Position) string {
	width := s.leftWidth()
	if l != nil {
		help := leftHelp(l.Help.Styles, width)
		l.SetSize(width, s.height-2-lipgloss.Height(help))
		msg = l.View() + "\n\n" + help
	}
	return borderStyle.
		Height(s.height).
		MaxHeight(s.height).
		Width(width + borderStyle.GetHorizontalBorderSize()).
		AlignHorizontal(align).
		Render(msg)
}

func leftHelp(h help.Styles, width int) string {
	hints := []string{
		h.ShortKey.Inline(true).Render("/") + " " + h.ShortDesc.Inline(true).Render("filter"),
		h.ShortKey.Inline(true).Render("s") + " " + h.ShortDesc.Inline(true).Render("sort"),
	}
	text := strings.Join(hints, h.ShortSeparator.Inline(true).Render(" • "))
	if lipgloss.Width(text) > width-leftHelpIndent {
		text = strings.Join(hints, "\n")
	}
	return lipgloss.NewStyle().
		PaddingLeft(max(0, min(leftHelpIndent, width-lipgloss.Width(text)))).
		Render(text)
}

func (s paneSize) rightPane(title, note string, body func() string, keys string) string {
	width := s.rightWidth()
	title = fit(printable(title), width-lipgloss.Width(note)) + note
	parts := []string{
		lipgloss.NewStyle().
			Width(width).
			AlignHorizontal(lipgloss.Center).
			Render(fit(title, width)),
		"",
	}
	if height := s.listHeight(); height > 0 {
		parts = append(parts, lipgloss.NewStyle().
			Width(width).
			Height(height).
			MaxHeight(height).
			Render(body()), "")
	}
	parts = append(parts, keys)

	return lipgloss.NewStyle().
		PaddingLeft(rightViewPadding).
		MaxWidth(width + rightViewPadding).
		MaxHeight(s.height).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (s paneSize) notice(text string) string {
	return lipgloss.NewStyle().
		Width(s.rightWidth()).
		PaddingTop(1).
		AlignHorizontal(lipgloss.Center).
		Render(text)
}

// keyHelp shows the keys of the right pane. tab, which moves to target, is
// left out when target is empty, as on tabs with only one list. The keys
// that control Spotify on every tab show in the player, or here, as many as
// fit, when the compact player has no room for them.
func (s paneSize) keyHelp(h help.Styles, target string) string {
	hint := func(key, desc string) string {
		return h.ShortKey.Inline(true).Render(key) + " " + h.ShortDesc.Inline(true).Render(desc)
	}
	hints := []string{hint("enter", "play"), hint("r", "refresh")}
	if target != "" {
		hints = append([]string{hint("tab", target)}, hints...)
	}
	separator := h.ShortSeparator.Inline(true).Render(" • ")
	text := strings.Join(hints, separator)
	if s.spotifyKeys {
		for _, ph := range playerHints {
			next := separator + hint(ph.key, ph.desc)
			if lipgloss.Width(text+next) > s.rightWidth() {
				break
			}
			text += next
		}
	}
	return lipgloss.NewStyle().
		Width(s.rightWidth()).
		AlignHorizontal(lipgloss.Center).
		Render(fit(text, s.rightWidth()))
}
