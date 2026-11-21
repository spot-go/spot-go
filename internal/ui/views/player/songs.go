package player

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

type (
	songItem struct {
		spotify.PlaylistItem
	}

	songDelegate struct {
		focused bool
	}
)

var (
	songStyle         = lipgloss.NewStyle()
	selectedSongStyle = lipgloss.NewStyle().
				Foreground(styles.LipglossColorForegroundGreenSpotify)
)

func (s songItem) FilterValue() string { return s.Name }

func (s songItem) label() string {
	if len(s.Artists) == 0 {
		return printable(s.Name)
	}
	return printable(s.Name + " - " + s.Artist())
}

func (d songDelegate) Height() int                         { return 1 }
func (d songDelegate) Spacing() int                        { return 0 }
func (d songDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d songDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	song, ok := li.(songItem)
	if !ok {
		return
	}

	cursor, style := "  ", songStyle
	if index == m.Index() {
		cursor = "> "
		if d.focused {
			style = selectedSongStyle
		}
	}
	if !song.Playable {
		style = style.Faint(true)
	}

	number := fmt.Sprintf("%*d  ", positionWidth(m), song.Position+1)
	duration := formatDuration(song.Duration)
	labelWidth := m.Width() - lipgloss.Width(cursor) - lipgloss.Width(number) - lipgloss.Width(duration) - 1
	label := fit(song.label(), labelWidth)
	gap := strings.Repeat(" ", max(0, labelWidth-lipgloss.Width(label))+1)

	fmt.Fprint(w, style.Render(cursor+number+label+gap+duration))
}

func positionWidth(m list.Model) int {
	items := m.Items()
	if len(items) == 0 {
		return 3
	}
	last, _ := items[len(items)-1].(songItem)
	return max(3, len(strconv.Itoa(last.Position+1)))
}

func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	hours := int(d / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	seconds := int(d % time.Minute / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\uFE0E' || r == '\uFE0F':
			return -1
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsPrint(r):
			return r
		}
		return -1
	}, s)
}
