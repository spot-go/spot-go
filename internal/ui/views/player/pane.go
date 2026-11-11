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
	width  int
	height int
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

func (s paneSize) rightPane(title, note string, body func() string, status playbackStatus, volume volumeLevel, keys string) string {
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
			Render(body()))
	}
	parts = append(parts, s.statusLine(status, volume), keys)

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

func (s paneSize) statusLine(status playbackStatus, volume volumeLevel) string {
	width, now := s.rightWidth(), time.Now()
	if !volume.shown {
		return status.view(width, now)
	}
	levelStyle := playingStyle
	if volume.err != nil {
		levelStyle = playbackErrorStyle
	}
	level := fit(printable(volume.text()), width)
	text := status.view(width-lipgloss.Width(level)-1, now)
	gap := strings.Repeat(" ", max(0, width-lipgloss.Width(text)-lipgloss.Width(level)))
	return text + gap + levelStyle.Render(level)
}

func (s paneSize) keyHelp(h help.Styles, target string) string {
	separator := h.ShortSeparator.Inline(true).Render(" • ")
	text := h.ShortKey.Inline(true).Render("tab") + " " + h.ShortDesc.Inline(true).Render(target) +
		separator +
		h.ShortKey.Inline(true).Render("enter") + " " + h.ShortDesc.Inline(true).Render("play") +
		separator +
		h.ShortKey.Inline(true).Render("r") + " " + h.ShortDesc.Inline(true).Render("refresh") +
		separator +
		h.ShortKey.Inline(true).Render("space") + " " + h.ShortDesc.Inline(true).Render("play/pause")
	volume := separator + h.ShortKey.Inline(true).Render("+/-") + " " + h.ShortDesc.Inline(true).Render("volume")
	if lipgloss.Width(text+volume) <= s.rightWidth() {
		text += volume
	}
	return lipgloss.NewStyle().
		Width(s.rightWidth()).
		AlignHorizontal(lipgloss.Center).
		Render(fit(text, s.rightWidth()))
}
