package player

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/spotify"
)

type (
	discographyItem struct {
		spotify.Album
	}

	discographyDelegate struct {
		focused bool
	}
)

const (
	discographyTypeWidth    = len("Compilation")
	discographyYearWidth    = 4
	minDiscographyNameWidth = 16
)

func (a discographyItem) FilterValue() string { return a.Name }

func (d discographyDelegate) Height() int                         { return 1 }
func (d discographyDelegate) Spacing() int                        { return 0 }
func (d discographyDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d discographyDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	album, ok := li.(discographyItem)
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

	year := fmt.Sprintf("%*s", discographyYearWidth, fit(discographyYear(album.Album), discographyYearWidth))
	nameWidth := m.Width() - lipgloss.Width(cursor) - 1 - discographyYearWidth
	kind := ""
	if nameWidth-discographyTypeWidth-2 >= minDiscographyNameWidth {
		nameWidth -= discographyTypeWidth + 2
		kind = fit(discographyType(album.Album), discographyTypeWidth)
		kind += strings.Repeat(" ", discographyTypeWidth-lipgloss.Width(kind)+2)
	}
	name := fit(printable(discographyName(album.Album)), nameWidth)
	gap := strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(name))+1)

	fmt.Fprint(w, fit(style.Render(cursor+name+gap+kind+year), m.Width()))
}

func discographyName(a spotify.Album) string {
	if a.Name == "" {
		return string(a.ID)
	}
	return a.Name
}

func discographyType(a spotify.Album) string {
	kind := strings.ToLower(printable(a.Type))
	r, size := utf8.DecodeRuneInString(kind)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(r)) + kind[size:]
}

func discographyYear(a spotify.Album) string {
	if a.Released.IsZero() || a.Released.Year() < 1 {
		return ""
	}
	return strconv.Itoa(a.Released.Year())
}

func discographyItems(albums []spotify.Album) []list.Item {
	items := make([]list.Item, len(albums))
	for i, a := range albums {
		items[i] = discographyItem{a}
	}
	return items
}

func listedDiscography(l *list.Model) []spotify.Album {
	if l == nil {
		return nil
	}
	albums := make([]spotify.Album, 0, len(l.Items()))
	for _, li := range l.Items() {
		if a, ok := li.(discographyItem); ok {
			albums = append(albums, a.Album)
		}
	}
	return albums
}

func unseenDiscography(seen, page []spotify.Album) []spotify.Album {
	uris := make(map[string]bool, len(seen)+len(page))
	for _, a := range seen {
		uris[string(a.URI)] = true
	}
	var unseen []spotify.Album
	for _, a := range page {
		if !uris[string(a.URI)] {
			uris[string(a.URI)] = true
			unseen = append(unseen, a)
		}
	}
	return unseen
}

func reselectDiscography(previous *list.Model, albums []spotify.Album) int {
	if previous == nil {
		return 0
	}
	index := min(previous.Index(), max(0, len(albums)-1))
	selected, ok := previous.SelectedItem().(discographyItem)
	if !ok {
		return index
	}
	for i, a := range albums {
		if a.URI == selected.URI {
			return i
		}
	}
	return index
}
