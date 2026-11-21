package player

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/franciscocpg/spot-go/internal/spotify"
)

type (
	episodeItem struct {
		spotify.Episode
	}

	episodeDelegate struct {
		focused bool
	}
)

const (
	episodeDateWidth     = 12
	episodeTimeWidth     = 12
	minEpisodeTitleWidth = 16
)

func (e episodeItem) FilterValue() string { return e.Name }

func (d episodeDelegate) Height() int                         { return 1 }
func (d episodeDelegate) Spacing() int                        { return 0 }
func (d episodeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d episodeDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	ep, ok := li.(episodeItem)
	if !ok {
		return
	}

	cursor, style, markStyle := "  ", songStyle, playingStyle
	if index == m.Index() {
		cursor = "> "
		if d.focused {
			style = selectedSongStyle
		}
		markStyle = style
	}
	if !ep.Playable {
		style = style.Faint(true)
		markStyle = markStyle.Faint(true)
	}

	mark := "  "
	if ep.FullyPlayed {
		mark = "✓ "
	}
	length := fmt.Sprintf("%*s", episodeTimeWidth, fit(episodeTime(ep.Episode), episodeTimeWidth))
	titleWidth := m.Width() - lipgloss.Width(cursor) - lipgloss.Width(mark) - 1 - episodeTimeWidth
	date := ""
	if titleWidth-episodeDateWidth-2 >= minEpisodeTitleWidth {
		titleWidth -= episodeDateWidth + 2
		date = fmt.Sprintf("%-*s  ", episodeDateWidth, releaseLabel(ep.Episode, time.Now()))
	}
	title := fit(printable(ep.Name), titleWidth)
	gap := strings.Repeat(" ", max(0, titleWidth-lipgloss.Width(title))+1)

	fmt.Fprint(w, fit(style.Render(cursor)+markStyle.Render(mark)+style.Render(title+gap+date+length), m.Width()))
}

func episodeItems(episodes []spotify.Episode) []list.Item {
	items := make([]list.Item, len(episodes))
	for i, e := range episodes {
		items[i] = episodeItem{e}
	}
	return items
}

func listedEpisodes(l *list.Model) []spotify.Episode {
	if l == nil {
		return nil
	}
	episodes := make([]spotify.Episode, 0, len(l.Items()))
	for _, li := range l.Items() {
		if e, ok := li.(episodeItem); ok {
			episodes = append(episodes, e.Episode)
		}
	}
	return episodes
}

func unseenEpisodes(seen, page []spotify.Episode) []spotify.Episode {
	uris := make(map[string]bool, len(seen)+len(page))
	for _, e := range seen {
		uris[string(e.URI)] = true
	}
	var unseen []spotify.Episode
	for _, e := range page {
		if !uris[string(e.URI)] {
			uris[string(e.URI)] = true
			unseen = append(unseen, e)
		}
	}
	return unseen
}

func releaseLabel(e spotify.Episode, now time.Time) string {
	if e.Released.IsZero() {
		return ""
	}
	switch e.Precision {
	case "day":
		if e.Released.Year() == now.Year() {
			return e.Released.Format("Jan 2")
		}
		return e.Released.Format("Jan 2, 2006")
	case "month":
		return e.Released.Format("Jan 2006")
	}
	return e.Released.Format("2006")
}

func episodeTime(e spotify.Episode) string {
	if e.InProgress() {
		return remainingLabel(e.Duration - e.Resume)
	}
	return formatDuration(e.Duration)
}

func remainingLabel(d time.Duration) string {
	minutes := int((d + time.Minute - 1) / time.Minute)
	switch {
	case minutes < 60:
		return fmt.Sprintf("%dm left", minutes)
	case minutes%60 == 0:
		return fmt.Sprintf("%dh left", minutes/60)
	}
	return fmt.Sprintf("%dh %dm left", minutes/60, minutes%60)
}
