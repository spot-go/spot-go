package player

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

type (
	// keyList is the list of every key, which ? shows over the tab: the
	// keys of the open tab, the keys that control Spotify and the other keys
	// of spot-go.
	keyList struct {
		open bool
		// offset is the first line of the list that shows, when the list
		// doesn't fit the window.
		offset int
	}

	// keySection is a part of the list of keys, under its title.
	keySection struct {
		title string
		keys  []playerHint
	}
)

const (
	keyListTitle = "Keys"
	// keyListFrame is how many columns the frame of the list takes around
	// its lines: "│ " on the left and " │" on the right.
	keyListFrame = 4
	// keyListBorders is how many lines the frame of the list takes: its top
	// and bottom lines.
	keyListBorders = 2
	keyColumnGap   = "    "
	keyDescGap     = "  "
)

var (
	// keyListHint is the hint for ?, which shows the list of keys.
	keyListHint = playerHint{"?", "keys"}

	// listKeys move in the lists of the tabs.
	listKeys = []playerHint{
		{"↑/↓", "move in the list"},
		{"pgup/pgdown", "previous/next page"},
		{"home/end", "first/last item"},
	}

	queueKeys = slices.Concat([]playerHint{
		{"enter", "skip to the selected song or episode"},
		{"r", "reload the queue"},
	}, listKeys)

	searchKeys = []playerHint{
		{"/", "type a search"},
		{"enter", "search, or play the selected result"},
		{"esc", "leave the search without searching"},
		{"tab", "move between categories and results"},
		{"↑/↓", "move in the list"},
		{"pgup/pgdown", "previous/next page of results"},
		{"home/end", "first/last result"},
		{"r", "search again"},
	}

	playlistKeys = libraryKeys("playlists", "songs", "play the playlist, or from the selected song")
	podcastKeys  = libraryKeys("podcasts", "episodes", "play the newest episode, or the selected one")
	artistKeys   = libraryKeys("artists", "albums", "play the artist, or the selected album")
	albumKeys    = libraryKeys("albums", "songs", "play the album, or from the selected song")

	// playbackKeys control Spotify on every tab.
	playbackKeys = []playerHint{
		{"space/k", "play/pause"},
		{"n", "next song or episode"},
		{"p", "previous song or episode"},
		{">/.", "forward 10 seconds"},
		{"</,", "back 10 seconds"},
		{"+/=", "volume up"},
		{"-", "volume down"},
		{"ctrl+s", "shuffle on/off"},
		{"ctrl+r", "next repeat mode"},
	}

	generalKeys = []playerHint{
		{"←/→", "switch tabs"},
		{"?", "show/hide these keys"},
		{"ctrl+c/ctrl+q", "quit"},
	}

	keySectionStyle = paneTitleStyle
)

// libraryKeys are the keys of a tab with a list of what is in the library on
// the left, items, and the list of what the selected one has on the right,
// contents.
func libraryKeys(items, contents, play string) []playerHint {
	return slices.Concat([]playerHint{
		{"tab", "move between " + items + " and " + contents},
		{"enter", play},
		{"/", "filter the " + items + " (enter applies)"},
		{"esc", "clear the filter"},
		{"s", "sort the " + items},
		{"r", "reload the " + contents},
	}, listKeys)
}

// keyListKey handles a key pressed while the list of keys shows, and reports
// whether it took it: ? and esc hide the list, and the keys that move in a
// list scroll it. The keys that control Spotify, switch tabs and quit work
// as on the tab, and the other keys don't reach the tab under the list.
func (m Model) keyListKey(msg tea.KeyPressMsg) (Model, bool) {
	switch msg.String() {
	case "?", "esc":
		m.keyList = keyList{}
	case "up":
		m = m.scrollKeyList(-1)
	case "down":
		m = m.scrollKeyList(1)
	case "pgup":
		m = m.scrollKeyList(-m.keyListPage())
	case "pgdown":
		m = m.scrollKeyList(m.keyListPage())
	case "home":
		m.keyList.offset = 0
	case "end":
		m = m.scrollKeyList(m.keyListEnd())
	default:
		return m, false
	}
	return m, true
}

// scrollKeyList moves the list of keys by lines, without going past its
// start or its end.
func (m Model) scrollKeyList(lines int) Model {
	end := m.keyListEnd()
	m.keyList.offset = max(0, min(end, min(m.keyList.offset, end)+lines))
	return m
}

// keyListEnd is the offset that shows the end of the list of keys.
func (m Model) keyListEnd() int {
	cd := m.contentDimension()
	lines, shown := m.keyListLines(cd.width, cd.height)
	return max(0, len(lines)-shown)
}

// keyListPage is how many lines of the list of keys show at once.
func (m Model) keyListPage() int {
	cd := m.contentDimension()
	_, shown := m.keyListLines(cd.width, cd.height)
	return max(1, shown)
}

// keySections are the parts of the list of keys: the keys of the open tab,
// then the keys that control Spotify and the other keys of spot-go.
func (m Model) keySections() []keySection {
	var sections []keySection
	if t := m.tabs[m.activeTab]; len(t.keys) > 0 {
		sections = append(sections, keySection{title: t.name + " tab", keys: t.keys})
	}
	return append(sections,
		keySection{title: "Playback", keys: playbackKeys},
		keySection{title: "General", keys: generalKeys},
	)
}

// keyListLines are the lines of the list of keys in an area width columns
// wide and height lines high, and how many of them show at once.
func (m Model) keyListLines(width, height int) ([]string, int) {
	h := help.DefaultStyles(styles.HasDarkBackground)
	lines := keyColumns(h, m.keySections(), width-keyListFrame)
	return lines, max(0, min(len(lines), height-keyListBorders))
}

// keyListView is the list of keys in a frame at the center of an area width
// columns wide and height lines high, with its title in the top line of the
// frame and the keys that move it in the bottom line, as the player shows
// its keys. When the list doesn't fit, it shows the lines from its offset,
// and ↑ and ↓ scroll it.
func (m Model) keyListView(width, height int) string {
	h := help.DefaultStyles(styles.HasDarkBackground)
	lines, shown := m.keyListLines(width, height)
	offset := max(0, min(m.keyList.offset, len(lines)-shown))
	hints := keyListHints(h, shown < len(lines), width-keyListFrame-2)
	title := titleStyle.Render(keyListTitle)

	boxWidth := min(width, max(
		widest(lines)+keyListFrame,
		lipgloss.Width(title)+keyListFrame+2,
		lipgloss.Width(hints)+keyListFrame+2,
	))
	inner := max(0, boxWidth-keyListFrame)
	rows := []string{frameEdge("╭", "╮", boxWidth, title, "")}
	for _, line := range lines[offset : offset+shown] {
		line = fit(line, inner)
		rows = append(rows, frameStyle.Render("│")+" "+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+" "+frameStyle.Render("│"))
	}
	rows = append(rows, frameEdge("╰", "╯", boxWidth, hints, ""))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(rows, "\n"))
}

// keyColumns lays out the sections of the list of keys in at most width
// columns. The keys of the open tab, the first section, show on the left of
// the others when both fit side by side, or else above them.
func keyColumns(h help.Styles, sections []keySection, width int) []string {
	if len(sections) > 1 {
		left, right := keyLines(h, sections[:1], 0), keyLines(h, sections[1:], 0)
		leftWidth := widest(left)
		if leftWidth+len(keyColumnGap)+widest(right) <= width {
			lines := make([]string, max(len(left), len(right)))
			for i := range lines {
				var l, r string
				if i < len(left) {
					l = left[i]
				}
				if i < len(right) {
					r = right[i]
				}
				if r != "" {
					l += strings.Repeat(" ", leftWidth-lipgloss.Width(l)) + keyColumnGap + r
				}
				lines[i] = l
			}
			return lines
		}
	}
	lines := keyLines(h, sections, width)
	for i, line := range lines {
		lines[i] = fit(line, width)
	}
	return lines
}

// keyLines are the lines of sections, one under the other, with the keys
// lined up in a column and what they do next to them. In at most width
// columns, what a key does goes on under itself when it doesn't fit, and
// with no width, it stays on one line.
func keyLines(h help.Styles, sections []keySection, width int) []string {
	keyWidth := 0
	for _, s := range sections {
		for _, k := range s.keys {
			keyWidth = max(keyWidth, lipgloss.Width(k.key))
		}
	}
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, keySectionStyle.Render(s.title))
		for _, k := range s.keys {
			desc := []string{k.desc}
			if width > 0 {
				desc = strings.Split(ansi.Wordwrap(k.desc, max(1, width-keyWidth-len(keyDescGap)), ""), "\n")
				for i, d := range desc {
					desc[i] = strings.TrimRight(d, " ")
				}
			}
			gap := strings.Repeat(" ", keyWidth-lipgloss.Width(k.key)) + keyDescGap
			lines = append(lines, h.FullKey.Inline(true).Render(k.key)+gap+h.FullDesc.Inline(true).Render(desc[0]))
			for _, more := range desc[1:] {
				lines = append(lines, strings.Repeat(" ", keyWidth+len(keyDescGap))+h.FullDesc.Inline(true).Render(more))
			}
		}
	}
	return lines
}

// keyListHints are the keys that move the list of keys, in at most width
// columns. ↑/↓ shows only when the list doesn't fit. The hints that don't
// fit are left out, esc last.
func keyListHints(h help.Styles, scrolls bool, width int) string {
	hints := []playerHint{{"←/→", "other tabs"}, {"esc", "close"}}
	if scrolls {
		hints = append([]playerHint{{"↑/↓", "scroll"}}, hints...)
	}
	for len(hints) > 0 {
		if line := hintText(h, hints); lipgloss.Width(line) <= width {
			return line
		}
		hints = hints[1:]
	}
	return ""
}

// hintText shows hints as the help of the tabs does: each key with what it
// does, between separators.
func hintText(h help.Styles, hints []playerHint) string {
	parts := make([]string, len(hints))
	for i, hint := range hints {
		parts[i] = h.ShortKey.Inline(true).Render(hint.key) + " " + h.ShortDesc.Inline(true).Render(hint.desc)
	}
	return strings.Join(parts, h.ShortSeparator.Inline(true).Render(" • "))
}

// widest is the width of the widest of lines.
func widest(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	return width
}
