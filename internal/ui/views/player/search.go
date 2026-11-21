package player

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	libspotify "github.com/zmb3/spotify/v2"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type (
	searchModel struct {
		playerContext
		paneSize
		input     textinput.Model
		help      help.Styles
		typing    bool
		leftFocus bool
		category  int
		query     string
		gen       int
		state     searchState
		err       error
		results   map[spotify.SearchType]*searchResults
		playback  playbackStatus
	}

	searchState int

	searchCategory struct {
		kind     spotify.SearchType
		name     string
		singular string
		plural   string
	}

	searchResults struct {
		list    *list.Model
		page    spotify.SearchPage
		loading bool
		err     error
	}

	searchItem struct {
		kind     spotify.SearchType
		id       libspotify.ID
		uri      libspotify.URI
		name     string
		artist   string
		detail   string
		duration time.Duration
		resume   time.Duration
		playable bool
		mixed    bool
		top      bool
	}

	searchDelegate struct {
		focused bool
	}

	searchLoaded struct {
		gen     int
		results spotify.SearchResults
	}

	searchFailed struct {
		gen int
		err error
	}

	searchPageLoaded struct {
		gen     int
		kind    spotify.SearchType
		results spotify.SearchResults
	}

	searchPageFailed struct {
		gen  int
		kind spotify.SearchType
		err  error
	}

	searchPlaybackStarted struct {
		seq      int
		uri      libspotify.URI
		item     libspotify.URI
		name     string
		artist   string
		duration time.Duration
		from     time.Duration
	}

	searchPlaybackFailed struct {
		seq int
		err error
	}
)

// searchAll is the All results: a mix of the other kinds, which the Web API
// has no type for.
const searchAll spotify.SearchType = "all"

const (
	stateSearchIdle = searchState(iota)
	stateSearchLoading
	stateSearchError
	stateSearchLoaded
)

const (
	searchTimeout   = 30 * time.Second
	searchPrefetch  = 5
	searchHelpText  = "Press / and type what you want to listen to."
	searchInputHint = "What to play?"
)

var (
	searchCategories = []searchCategory{
		{searchAll, "All", "result", "results"},
		{spotify.SearchSongs, "Songs", "song", "songs"},
		{spotify.SearchArtists, "Artists", "artist", "artists"},
		{spotify.SearchAlbums, "Albums", "album", "albums"},
		{spotify.SearchPlaylists, "Playlists", "playlist", "playlists"},
		{spotify.SearchShows, "Podcasts", "podcast", "podcasts"},
		{spotify.SearchEpisodes, "Episodes", "episode", "episodes"},
	}
	combiningAccents = &unicode.RangeTable{R16: []unicode.Range16{{Lo: 0x0300, Hi: 0x036F, Stride: 1}}}
	searchKindLabels = map[spotify.SearchType]string{
		spotify.SearchSongs:     "Song",
		spotify.SearchArtists:   "Artist",
		spotify.SearchAlbums:    "Album",
		spotify.SearchPlaylists: "Playlist",
		spotify.SearchShows:     "Podcast",
		spotify.SearchEpisodes:  "Episode",
	}
)

func newSearchModel(ctx playerContext) searchModel {
	input := textinput.New()
	input.Placeholder = searchInputHint
	input.Prompt = "> "
	inputStyles := textinput.DefaultStyles(styles.HasDarkBackground)
	inputStyles.Focused.Prompt = playingStyle
	inputStyles.Cursor.Color = styles.LipglossColorForegroundGreenSpotify
	inputStyles.Cursor.Blink = false
	input.SetStyles(inputStyles)

	return searchModel{
		playerContext: ctx,
		input:         input,
		help:          styles.Help(),
		leftFocus:     true,
		results:       make(map[spotify.SearchType]*searchResults),
	}
}

func (s searchItem) FilterValue() string { return s.name }

func (s searchItem) label() string {
	if s.artist == "" || !s.mixed && (s.kind == spotify.SearchShows || s.kind == spotify.SearchPlaylists) {
		return printable(s.name)
	}
	return printable(s.name + " - " + s.artist)
}

func (s searchItem) heading() string {
	switch s.kind {
	case spotify.SearchPlaylists:
		if s.artist != "" {
			return s.name + " by " + s.artist
		}
	case spotify.SearchShows:
		if s.artist != "" {
			return s.name + " - " + s.artist
		}
	}
	return s.label()
}

func (d searchDelegate) Height() int                         { return 1 }
func (d searchDelegate) Spacing() int                        { return 0 }
func (d searchDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d searchDelegate) Render(w io.Writer, m list.Model, index int, li list.Item) {
	item, ok := li.(searchItem)
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
	if !item.playable {
		style = style.Faint(true)
	}
	if item.top {
		style = style.Bold(true)
	}

	detail := fit(printable(item.detail), max(0, m.Width()/3))
	labelWidth := m.Width() - lipgloss.Width(cursor) - lipgloss.Width(detail) - 1
	label := fit(item.label(), labelWidth)
	gap := strings.Repeat(" ", max(0, labelWidth-lipgloss.Width(label))+1)

	fmt.Fprint(w, fit(style.Render(cursor+label+gap+detail), m.Width()))
}

func searchItems(kind spotify.SearchType, r spotify.SearchResults) []list.Item {
	var items []list.Item
	switch kind {
	case spotify.SearchSongs:
		for _, s := range r.Songs {
			items = append(items, searchItem{kind: kind, uri: s.URI, name: s.Name, artist: s.Artist(), detail: formatDuration(s.Duration), duration: s.Duration, playable: s.Playable})
		}
	case spotify.SearchArtists:
		for _, a := range r.Artists {
			items = append(items, searchItem{kind: kind, id: a.ID, uri: a.URI, name: a.Name, playable: true})
		}
	case spotify.SearchAlbums:
		for _, a := range r.Albums {
			detail := strings.TrimSpace(discographyType(a) + "  " + discographyYear(a))
			items = append(items, searchItem{kind: kind, id: a.ID, uri: a.URI, name: discographyName(a), artist: strings.Join(a.Artists, ", "), detail: detail, playable: true})
		}
	case spotify.SearchPlaylists:
		for _, p := range r.Playlists {
			items = append(items, searchItem{kind: kind, id: p.ID, uri: p.URI, name: p.Name, artist: p.Owner, detail: p.Owner, playable: true})
		}
	case spotify.SearchShows:
		for _, sh := range r.Shows {
			items = append(items, searchItem{kind: kind, id: sh.ID, uri: sh.URI, name: sh.Name, artist: sh.Publisher, detail: sh.Publisher, playable: true})
		}
	case spotify.SearchEpisodes:
		now := time.Now()
		for _, e := range r.Episodes {
			detail := strings.TrimSpace(releaseLabel(e, now) + "  " + episodeTime(e))
			if e.FullyPlayed {
				detail = "✓ " + detail
			}
			item := searchItem{kind: kind, id: e.ID, uri: e.URI, name: e.Name, detail: detail, duration: e.Duration, playable: e.Playable}
			if e.InProgress() {
				item.resume = e.Resume
			}
			items = append(items, item)
		}
	}
	return items
}

func (s searchModel) Init() tea.Cmd {
	return nil
}

func (s searchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return s.handleKey(msg)

	case contentDimension:
		s.paneSize = paneSize(msg)
		s.input.SetWidth(max(1, s.leftWidth()-lipgloss.Width(s.input.Prompt)-3))
		for _, r := range s.results {
			if r.list != nil {
				s.resizeRightList(r.list)
			}
		}
		return s, nil

	case searchLoaded:
		if msg.gen != s.gen {
			return s, nil
		}
		s.loaded(msg.results)
		return s, s.more()

	case searchFailed:
		if msg.gen != s.gen {
			return s, nil
		}
		s.state, s.err = stateSearchError, msg.err
		return s, nil

	case searchPageLoaded:
		return s, s.addPage(msg)

	case searchPageFailed:
		if r := s.results[msg.kind]; msg.gen == s.gen && r != nil && r.loading {
			r.loading, r.err = false, msg.err
		}
		return s, nil

	case searchPlaybackStarted:
		if msg.seq == s.playback.seq {
			s.playback = playingStatus(msg.seq, msg.item, msg.info(), time.Now())
		}
		return s, nil

	case searchPlaybackFailed:
		if msg.seq == s.playback.seq {
			s.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		return s, nil

	case playbackToggled:
		if msg.plays == *s.plays {
			s.playback = s.playback.toggled(msg, time.Now())
		}
		return s, nil

	case playbackConfirmed:
		s.playback = s.playback.followNow(msg.playing, time.Now())
		return s, nil

	case playbackPolled:
		if msg.err == nil && msg.plays == *s.plays {
			s.playback = s.playback.follow(msg.playing, time.Now())
		}
		return s, nil
	}

	if s.typing {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s searchModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s.typing {
		switch msg.String() {
		case "enter":
			query := strings.TrimSpace(s.input.Value())
			if query == "" {
				return s, nil
			}
			s.typing, s.leftFocus = false, false
			s.input.Blur()
			s.focusResults()
			return s, s.search(query)
		case "esc":
			// esc leaves the input without searching, even before the first
			// search, as every key goes to the input while typing: back to
			// the results, or to the hint, with the last search in the input.
			s.typing = false
			s.input.Blur()
			s.input.SetValue(s.query)
			s.input.CursorEnd()
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	}

	switch msg.String() {
	case "/":
		s.typing = true
		s.input.CursorEnd()
		return s, s.input.Focus()

	case "tab":
		s.leftFocus = !s.leftFocus
		s.focusResults()
		return s, nil

	case "r":
		if s.query != "" {
			return s, s.search(s.query)
		}
		return s, nil

	case "enter":
		if s.state == stateSearchIdle {
			s.typing = true
			return s, s.input.Focus()
		}
		if s.leftFocus {
			s.leftFocus = false
			s.focusResults()
			return s, nil
		}
		if s.state != stateSearchLoaded {
			return s, nil
		}
		return s.play()

	case "up", "down":
		// The categories show only once a search has loaded, so a key that
		// moved them before would leave the first search on a kind other
		// than All.
		if s.leftFocus && s.state == stateSearchLoaded {
			if msg.String() == "up" {
				s.category = max(0, s.category-1)
			} else {
				s.category = min(len(searchCategories)-1, s.category+1)
			}
			return s, s.more()
		}
	}

	if s.leftFocus || s.state != stateSearchLoaded {
		return s, nil
	}
	r := s.selectedResults()
	if r == nil || r.list == nil {
		return s, nil
	}
	l, cmd := r.list.Update(msg)
	r.list = &l
	return s, tea.Batch(cmd, s.more())
}

func (s searchModel) capturesInput() bool {
	return s.typing
}

func (s searchModel) status() playbackStatus {
	return s.playback
}

func (s *searchModel) search(query string) tea.Cmd {
	s.gen++
	s.query, s.state, s.err = query, stateSearchLoading, nil
	s.results = make(map[spotify.SearchType]*searchResults)
	gen, ctx, client := s.gen, s.ctx, s.spotifyClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, searchTimeout)
		defer cancel()

		results, err := client.Search(ctx, query, 0)
		if err != nil {
			return searchFailed{gen, err}
		}
		return searchLoaded{gen, results}
	}
}

func (s *searchModel) loaded(results spotify.SearchResults) {
	s.state = stateSearchLoaded
	s.results = make(map[spotify.SearchType]*searchResults, len(searchCategories))
	byKind := make(map[spotify.SearchType][]list.Item, len(searchCategories))
	for _, c := range searchCategories {
		if c.kind != searchAll {
			byKind[c.kind] = uniqueItems(nil, searchItems(c.kind, results))
		}
	}
	byKind[searchAll] = mixedItems(s.query, byKind)
	for _, c := range searchCategories {
		r := &searchResults{page: results.Pages[c.kind]}
		if c.kind == searchAll {
			r.page = spotify.SearchPage{Next: len(byKind[c.kind]), Total: len(byKind[c.kind]), Last: true}
		}
		r.list = s.newRightList(byKind[c.kind], searchDelegate{focused: !s.leftFocus && !s.typing}, c.singular, c.plural)
		s.results[c.kind] = r
	}
	if len(s.selectedResults().list.Items()) == 0 {
		s.category = s.firstWithResults()
	}
}

func (s searchModel) firstWithResults() int {
	for i, c := range searchCategories {
		if r := s.results[c.kind]; r != nil && r.list != nil && len(r.list.Items()) > 0 {
			return i
		}
	}
	return 0
}

func (s searchModel) more() tea.Cmd {
	kind := searchCategories[s.category].kind
	r := s.results[kind]
	if r == nil || r.list == nil || r.loading || r.page.Last || r.err != nil {
		return nil
	}
	if r.list.Index() < len(r.list.Items())-max(searchPrefetch, r.list.Paginator.PerPage) {
		return nil
	}
	r.loading = true
	gen, query, offset, ctx, client := s.gen, s.query, r.page.Next, s.ctx, s.spotifyClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, searchTimeout)
		defer cancel()

		results, err := client.Search(ctx, query, offset, kind)
		if err != nil {
			return searchPageFailed{gen, kind, err}
		}
		return searchPageLoaded{gen, kind, results}
	}
}

func (s searchModel) addPage(msg searchPageLoaded) tea.Cmd {
	r := s.results[msg.kind]
	if msg.gen != s.gen || r == nil || !r.loading {
		return nil
	}
	r.loading = false
	r.page = msg.results.Pages[msg.kind]
	r.list.SetItems(uniqueItems(r.list.Items(), searchItems(msg.kind, msg.results)))
	if searchCategories[s.category].kind != msg.kind {
		return nil
	}
	return s.more()
}

// mixedItems builds the All results from the first page of each kind, as
// the Web API has no mixed search. The top result comes first: the artist
// or podcast named exactly as the search, or else the first artist, or else
// the first podcast. Then come the albums, songs and playlists, taking the
// next one of each kind in turn, so the best matches of every kind show
// near the top; then the podcasts and episodes the same way. When the top
// result is a podcast, the podcasts and episodes come before the music.
func mixedItems(query string, byKind map[spotify.SearchType][]list.Item) []list.Item {
	music := []spotify.SearchType{spotify.SearchAlbums, spotify.SearchSongs, spotify.SearchPlaylists}
	talk := []spotify.SearchType{spotify.SearchShows, spotify.SearchEpisodes}

	var items []list.Item
	top, hasTop := topResult(query, byKind)
	if hasTop {
		top.mixed, top.top, top.detail = true, true, searchKindLabels[top.kind]
		items = append(items, top)
		if top.kind == spotify.SearchShows {
			music, talk = talk, music
		}
	}
	for _, group := range [][]spotify.SearchType{music, talk} {
		for rank := 0; ; rank++ {
			more := false
			for _, kind := range group {
				if rank >= len(byKind[kind]) {
					continue
				}
				more = true
				item := byKind[kind][rank].(searchItem)
				if hasTop && item.uri == top.uri {
					continue
				}
				item.mixed, item.detail = true, searchKindLabels[kind]
				items = append(items, item)
			}
			if !more {
				break
			}
		}
	}
	return items
}

// topResult picks the top result of the All results among the artists and
// podcasts: the first one named exactly as query, ignoring case and accents,
// or else the first artist, or else the first podcast.
func topResult(query string, byKind map[spotify.SearchType][]list.Item) (searchItem, bool) {
	kinds := []spotify.SearchType{spotify.SearchArtists, spotify.SearchShows}
	for _, kind := range kinds {
		for _, li := range byKind[kind] {
			if item := li.(searchItem); sameName(item.name, query) {
				return item, true
			}
		}
	}
	for _, kind := range kinds {
		if len(byKind[kind]) > 0 {
			return byKind[kind][0].(searchItem), true
		}
	}
	return searchItem{}, false
}

func sameName(a, b string) bool {
	return foldName(a) == foldName(b)
}

// foldName drops case, accents and extra spaces, so "Tim  Bernardes" and
// "tim bernardes" are the same name. Only the accents of the Combining
// Diacritical Marks block go, as other marks, such as the kana voicing
// marks or Indic vowel signs, change the letter.
func foldName(s string) string {
	folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(combiningAccents)), norm.NFC), s)
	if err != nil {
		folded = s
	}
	return strings.Join(strings.Fields(strings.ToLower(folded)), " ")
}

// uniqueItems returns a copy of listed followed by the items of page whose
// URI isn't listed yet, so every URI shows once.
func uniqueItems(listed, page []list.Item) []list.Item {
	items := slices.Clone(listed)
	seen := make(map[libspotify.URI]bool, len(listed)+len(page))
	for _, li := range listed {
		seen[li.(searchItem).uri] = true
	}
	for _, li := range page {
		if uri := li.(searchItem).uri; !seen[uri] {
			seen[uri] = true
			items = append(items, li)
		}
	}
	return items
}

func (s searchModel) selectedResults() *searchResults {
	return s.results[searchCategories[s.category].kind]
}

func (s searchModel) selectedItem() (searchItem, bool) {
	r := s.selectedResults()
	if r == nil || r.list == nil {
		return searchItem{}, false
	}
	item, ok := r.list.SelectedItem().(searchItem)
	return item, ok
}

func (s searchModel) focusResults() {
	for _, r := range s.results {
		if r.list != nil {
			r.list.SetDelegate(searchDelegate{focused: !s.leftFocus && !s.typing})
		}
	}
}

func (s searchModel) play() (tea.Model, tea.Cmd) {
	item, ok := s.selectedItem()
	if !ok {
		return s, nil
	}
	if !item.playable {
		s.playback = playbackStatus{seq: s.playback.seq + 1, text: item.name + " is unavailable", err: true}
		return s, nil
	}

	*s.plays++
	s.playback = playbackStatus{seq: s.playback.seq + 1, text: "Starting " + item.name + "...", starting: true}
	return s, s.startPlayback(s.playback.seq, item)
}

func (s searchModel) startPlayback(seq int, item searchItem) tea.Cmd {
	var deviceID libspotify.ID
	if s.playerDevice != nil {
		deviceID = s.playerDevice.ID
	}
	ctx, client := s.ctx, s.spotifyClient

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		started := searchPlaybackStarted{seq: seq, uri: item.uri, name: item.name, artist: item.artist}
		var err error
		switch item.kind {
		case spotify.SearchSongs:
			started.uri, started.item, started.duration = "", item.uri, item.duration
			err = client.PlayItem(ctx, deviceID, item.uri, 0)
		case spotify.SearchEpisodes:
			started.uri, started.item, started.duration, started.from = "", item.uri, item.duration, item.resume
			err = client.PlayItem(ctx, deviceID, item.uri, item.resume)
		case spotify.SearchArtists:
			started.artist = ""
			err = client.PlayArtist(ctx, deviceID, item.uri)
		case spotify.SearchShows:
			return playNewestEpisode(ctx, client, deviceID, seq, item)
		default:
			err = client.PlayContext(ctx, deviceID, item.uri, 0)
		}
		if err != nil {
			return searchPlaybackFailed{seq, err}
		}
		return started
	}
}

// playNewestEpisode plays the newest playable episode of show, looking only
// at its newest page of episodes, as the Podcasts tab does.
func playNewestEpisode(ctx context.Context, client *spotify.Client, deviceID libspotify.ID, seq int, show searchItem) tea.Msg {
	page, err := client.ShowEpisodesPage(ctx, show.id, 0)
	if err != nil {
		return searchPlaybackFailed{seq, err}
	}
	for _, ep := range page.Items {
		if !ep.Playable {
			continue
		}
		var from time.Duration
		if ep.InProgress() {
			from = ep.Resume
		}
		if err := client.PlayEpisode(ctx, deviceID, show.uri, ep.URI, from); err != nil {
			return searchPlaybackFailed{seq, err}
		}
		return searchPlaybackStarted{seq: seq, uri: show.uri, item: ep.URI, name: ep.Name, artist: show.name, duration: ep.Duration, from: from}
	}
	return searchPlaybackFailed{seq, fmt.Errorf("%s has no episodes to play", show.name)}
}

func (msg searchPlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Artist: msg.artist, Duration: msg.duration, Elapsed: msg.from, Playing: true}
}

func (s searchModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, s.leftView(), s.rightView()))
}

func (s searchModel) leftView() string {
	width := s.leftWidth()
	lines := []string{paneTitleStyle.Render("Search"), "", fit(s.input.View(), width), ""}
	if s.state == stateSearchLoaded {
		for i, c := range searchCategories {
			lines = append(lines, s.categoryLine(i, c, width))
		}
	}

	help := s.leftHelp(width)
	body := lipgloss.NewStyle().
		PaddingLeft(1).
		Height(max(0, s.height-lipgloss.Height(help)-2)).
		MaxHeight(max(0, s.height-lipgloss.Height(help)-2)).
		Render(strings.Join(lines, "\n"))
	return borderStyle.
		Height(s.height).
		MaxHeight(s.height).
		Width(width + borderStyle.GetHorizontalBorderSize()).
		Render(body + "\n\n" + help)
}

func (s searchModel) categoryLine(i int, c searchCategory, width int) string {
	cursor, style := "  ", songStyle
	if i == s.category {
		cursor = "> "
		if s.leftFocus && !s.typing {
			style = selectedSongStyle
		}
	}
	count := ""
	if r := s.results[c.kind]; r != nil {
		count = strconv.Itoa(r.page.Total)
		if r.list == nil || len(r.list.Items()) == 0 {
			style = style.Faint(true)
		}
	}
	room := width - 2 - lipgloss.Width(cursor)
	name := fit(c.name, room-lipgloss.Width(count)-1)
	gap := strings.Repeat(" ", max(1, room-lipgloss.Width(name)-lipgloss.Width(count)))
	return fit(style.Render(cursor+name+gap+count), width-1)
}

func (s searchModel) leftHelp(width int) string {
	h := s.help
	var hints []string
	if s.typing {
		hints = append(hints, h.ShortKey.Inline(true).Render("enter")+" "+h.ShortDesc.Inline(true).Render("search"))
		back := "results"
		if s.state == stateSearchIdle {
			back = "cancel"
		}
		hints = append(hints, h.ShortKey.Inline(true).Render("esc")+" "+h.ShortDesc.Inline(true).Render(back))
	} else {
		hints = append(hints, h.ShortKey.Inline(true).Render("/")+" "+h.ShortDesc.Inline(true).Render("search"))
	}
	text := strings.Join(hints, h.ShortSeparator.Inline(true).Render(" • "))
	if lipgloss.Width(text) > width-leftHelpIndent {
		text = strings.Join(hints, "\n")
	}
	return lipgloss.NewStyle().
		PaddingLeft(max(0, min(leftHelpIndent, width-lipgloss.Width(text)))).
		Render(text)
}

func (s searchModel) rightView() string {
	target := "results"
	if !s.leftFocus {
		target = "categories"
	}
	keys := s.keyHelp(s.help, target, s.typing)

	switch s.state {
	case stateSearchIdle:
		return s.rightPane("", "", func() string { return s.notice(searchHelpText) }, s.hintRow(s.help, nil, s.typing))
	case stateSearchLoading:
		return s.rightPane(`Searching for "`+s.query+`"...`, "", func() string { return "" }, keys)
	case stateSearchError:
		return s.rightPane("", "", func() string {
			return s.notice(fmt.Sprintf("Fail to search:\n%s\n\nPress r to try again.", errorText(s.err)))
		}, keys)
	}

	c := searchCategories[s.category]
	r := s.selectedResults()
	title := c.name + ` for "` + s.query + `"`
	if c.kind == searchAll {
		title = `Results for "` + s.query + `"`
	}
	if item, ok := s.selectedItem(); ok && !s.leftFocus {
		title = item.heading()
	}
	note := ""
	switch {
	case r.loading:
		note = fmt.Sprintf(" (loading %d of %d...)", len(r.list.Items()), r.page.Total)
	case r.err != nil:
		note = " (fail to load: " + errorText(r.err) + ")"
	}
	return s.rightPane(title, note, func() string {
		if len(r.list.Items()) == 0 {
			return s.notice(fmt.Sprintf("No %s found for \"%s\".", c.plural, s.query))
		}
		return r.list.View()
	}, keys)
}
