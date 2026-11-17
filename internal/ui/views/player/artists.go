package player

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	artistModel struct {
		playerContext
		paneSize
		spinner          spinner.Model
		leftFocus        bool
		alignHorizontal  lipgloss.Position
		msg              string
		state            artistState
		list             *list.Model
		discographies    map[libspotify.ID]*artistDiscography
		playback         playbackStatus
		sortPending      bool
		playingURI       libspotify.URI
		playedBeforeLoad map[libspotify.URI]time.Time
		after            func(time.Duration, tea.Msg) tea.Cmd
	}

	artistItem struct {
		title string
		id    libspotify.ID
		uri   libspotify.URI
		index int
	}

	artistDiscography struct {
		state      discographyState
		err        error
		list       *list.Model
		gen        int
		loaded     int
		total      int
		want       int
		last       bool
		loading    bool
		refreshing bool
		buffer     []spotify.Album
	}

	artistState int

	discographyState int

	artistsLoaded struct {
		artists []spotify.Artist
		recent  []spotify.PlayedContext
	}

	artistsLoadFailed struct {
		err error
	}

	discographyRequested struct {
		artistID libspotify.ID
	}

	discographyPageLoaded struct {
		artistID libspotify.ID
		gen      int
		page     spotify.AlbumsPage
	}

	discographyLoadFailed struct {
		artistID libspotify.ID
		gen      int
		err      error
	}

	artistPlaybackStarted struct {
		seq    int
		name   string
		artist libspotify.URI
		uri    libspotify.URI
	}

	artistPlaybackFailed struct {
		seq int
		err error
	}

	artistHistorySaveFailed struct {
		err error
	}
)

const (
	stateArtistsInit = artistState(iota)
	stateArtistsError
	stateArtistsLoaded
)

const (
	stateDiscographyLoading = discographyState(iota)
	stateDiscographyLoaded
	stateDiscographyError
)

const (
	artistsTimeout      = 45 * time.Second
	discographyPerPage  = 10
	discographyPrefetch = 10
	artistContextPrefix = "spotify:artist:"
	noArtists           = "You don't follow any artists.\n\nFollow artists in the Spotify app, then press r."
)

func (a artistItem) Title() string       { return a.title }
func (a artistItem) Description() string { return "" }
func (a artistItem) FilterValue() string { return a.title }

func newArtistModel(ctx playerContext, historyErr error) artistModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	a := artistModel{
		playerContext:    ctx,
		spinner:          s,
		alignHorizontal:  lipgloss.Center,
		leftFocus:        true,
		msg:              s.View(),
		discographies:    make(map[libspotify.ID]*artistDiscography),
		playedBeforeLoad: make(map[libspotify.URI]time.Time),
		after:            after,
	}
	if historyErr != nil {
		a.playback = playbackStatus{text: "Fail to read the artist history: " + errorText(historyErr), err: true}
	}
	return a
}

func (a artistModel) Init() tea.Cmd {
	return tea.Batch(a.spinner.Tick, a.followed)
}

func (a artistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m, cmd, handled := a.handleKey(msg); handled {
			return m, cmd
		}

	case contentDimension:
		a.paneSize = paneSize(msg)
		a.resizeDiscographies()
		if artist, ok := a.selectedArtist(); ok {
			return a, a.moreDiscography(artist.id)
		}
		return a, nil

	case artistsLoadFailed:
		a.state = stateArtistsError
		a.msg = fmt.Sprintf("Fail to load artists:\n%s\n\nPress r to try again.", errorText(msg.err))
		a.list = nil
		a.alignHorizontal = lipgloss.Center
		return a, nil

	case artistsLoaded:
		return a.loaded(msg)

	case discographyRequested:
		if selected, ok := a.selectedArtist(); ok && selected.id == msg.artistID {
			return a, a.loadDiscography(msg.artistID)
		}
		return a, nil

	case discographyPageLoaded:
		return a.addDiscographyPage(msg)

	case discographyLoadFailed:
		a.failDiscography(msg)
		return a, nil

	case artistPlaybackStarted:
		if msg.seq == a.playback.seq {
			a.playback = playingStatus(msg.seq, "", msg.info(), time.Now())
		}
		a.playingURI = msg.uri
		return a.played(msg.artist, time.Now())

	case artistPlaybackFailed:
		if msg.seq == a.playback.seq {
			a.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		return a, nil

	case playbackToggled:
		if msg.plays == *a.plays {
			a.playback = a.playback.toggled(msg, time.Now())
		}
		return a, nil

	case playbackConfirmed:
		a.playback = a.playback.followNow(msg.playing, time.Now())
		return a, nil

	case playbackPolled:
		if msg.err != nil {
			return a, nil
		}
		if msg.plays == *a.plays {
			a.playback = a.playback.follow(msg.playing, time.Now())
		}
		uri := msg.playing.Context
		if uri == a.playingURI {
			return a, nil
		}
		a.playingURI = uri
		if strings.HasPrefix(string(uri), artistContextPrefix) {
			return a.played(uri, time.Now())
		}
		return a, nil

	case artistHistorySaveFailed:
		a.playback = playbackStatus{seq: a.playback.seq, text: "Fail to save the artist history: " + errorText(msg.err), err: true}
		return a, nil
	}

	var cmd tea.Cmd
	switch a.state {
	case stateArtistsInit:
		a.spinner, cmd = a.spinner.Update(msg)
		a.msg = a.spinner.View()
		return a, cmd

	case stateArtistsLoaded:
		if _, isKey := msg.(tea.KeyPressMsg); isKey && !a.leftFocus {
			return a.updateDiscography(msg)
		}
		return a.updateArtists(msg)
	}

	return a, nil
}

func (a artistModel) loaded(msg artistsLoaded) (tea.Model, tea.Cmd) {
	a.state = stateArtistsLoaded

	items := make([]list.Item, 0, len(msg.artists))
	uris := make([]libspotify.URI, 0, len(msg.artists))
	for i, artist := range msg.artists {
		title := artist.Name
		if title == "" {
			title = string(artist.ID)
		}
		items = append(items, artistItem{title: title, id: artist.ID, uri: artist.URI, index: i})
		uris = append(uris, artist.URI)
	}
	a.history.SyncArtists(uris, time.Now())
	for _, played := range msg.recent {
		a.history.ArtistPlayed(played.URI, played.At)
	}
	for uri, at := range a.playedBeforeLoad {
		a.history.ArtistPlayed(uri, at)
	}
	clear(a.playedBeforeLoad)

	l := newLibraryList(sortArtists(items, a.order(), *a.history), orderLabels[a.order()], "artist", "artists")
	a.list = &l
	a.alignHorizontal = lipgloss.Left

	cmds := []tea.Cmd{a.saveHistory()}
	if selected, ok := a.selectedArtist(); ok {
		cmds = append(cmds, a.loadDiscography(selected.id))
	}
	return a, tea.Batch(cmds...)
}

func (a artistModel) handleKey(msg tea.KeyPressMsg) (artistModel, tea.Cmd, bool) {
	if a.capturesInput() {
		return a, nil, false
	}

	switch msg.String() {
	case "tab":
		a.leftFocus = !a.leftFocus
		a.focusDiscographies()
		return a, nil, true

	case "r":
		if a.state == stateArtistsError || (a.state == stateArtistsLoaded && len(a.list.Items()) == 0) {
			a.state = stateArtistsInit
			a.list = nil
			a.alignHorizontal = lipgloss.Center
			a.msg = a.spinner.View()
			return a, a.Init(), true
		}
		if selected, ok := a.selectedArtist(); ok {
			return a, a.refreshDiscography(selected.id), true
		}

	case "enter":
		if a.state == stateArtistsLoaded {
			m, cmd := a.play()
			return m, cmd, true
		}

	case "s":
		if a.state == stateArtistsLoaded {
			a.history.SetArtistOrder(nextOrder(artistOrders, a.order()))
			a = a.sorted()
			return a, a.saveHistory(), true
		}
	}

	return a, nil, false
}

func (a artistModel) updateArtists(msg tea.Msg) (tea.Model, tea.Cmd) {
	before, _ := a.selectedArtist()
	l, cmd := a.list.Update(msg)
	a.list = &l
	if a.sortPending {
		a = a.sorted()
	}

	current, ok := a.selectedArtist()
	if !ok || current.id == before.id {
		return a, cmd
	}
	return a, tea.Batch(cmd, a.scheduleDiscography(current.id))
}

func (a artistModel) updateDiscography(msg tea.Msg) (tea.Model, tea.Cmd) {
	artist, ok := a.selectedArtist()
	if !ok {
		return a, nil
	}
	d := a.discographies[artist.id]
	if d == nil || d.list == nil {
		return a, nil
	}
	l, cmd := d.list.Update(msg)
	d.list = &l
	return a, tea.Batch(cmd, a.moreDiscography(artist.id))
}

func (a artistModel) capturesInput() bool {
	return a.list != nil && a.list.FilterState() == list.Filtering
}

func (a artistModel) status() playbackStatus {
	return a.playback
}

func (a artistModel) selectedArtist() (artistItem, bool) {
	if a.list == nil {
		return artistItem{}, false
	}
	artist, ok := a.list.SelectedItem().(artistItem)
	return artist, ok
}

func (a artistModel) selectedDiscography() *artistDiscography {
	artist, ok := a.selectedArtist()
	if !ok {
		return nil
	}
	return a.discographies[artist.id]
}

func (a artistModel) selectedAlbum() (spotify.Album, bool) {
	d := a.selectedDiscography()
	if d == nil || d.list == nil {
		return spotify.Album{}, false
	}
	album, ok := d.list.SelectedItem().(discographyItem)
	return album.Album, ok
}

func (a artistModel) order() library.Order {
	return validOrder(artistOrders, a.history.Artists.Order)
}

func (a artistModel) sorted() artistModel {
	if a.list == nil {
		return a
	}
	a.list.Title = orderLabels[a.order()]
	if a.list.FilterState() == list.Filtering {
		a.sortPending = true
		return a
	}

	a.sortPending = false
	selected, ok := a.selectedArtist()
	a.list.SetItems(sortArtists(a.list.Items(), a.order(), *a.history))
	if a.list.FilterState() == list.FilterApplied {
		a.list.SetFilterText(a.list.FilterValue())
	}
	if ok {
		a.selectArtist(selected.id)
	}
	return a
}

func (a artistModel) selectArtist(id libspotify.ID) {
	for i, li := range a.list.VisibleItems() {
		if artist, ok := li.(artistItem); ok && artist.id == id {
			a.list.Select(i)
			return
		}
	}
}

func (a artistModel) played(uri libspotify.URI, at time.Time) (artistModel, tea.Cmd) {
	if !a.history.ArtistPlayed(uri, at) {
		if a.list == nil {
			a.playedBeforeLoad[uri] = at
		}
		return a, nil
	}
	if a.order() == library.OrderRecents {
		a = a.sorted()
	}
	return a, a.saveHistory()
}

func (a artistModel) saveHistory() tea.Cmd {
	store := a.historyStore
	if store == nil {
		return nil
	}
	h := a.history.Clone()
	return func() tea.Msg {
		if err := store.Save(h); err != nil {
			return artistHistorySaveFailed{err}
		}
		return nil
	}
}

func (a artistModel) followed() tea.Msg {
	ctx, cancel := context.WithTimeout(a.ctx, artistsTimeout)
	defer cancel()

	artists, err := a.spotifyClient.AllFollowedArtists(ctx)
	if err != nil {
		return artistsLoadFailed{err}
	}

	recentCtx, cancelRecent := context.WithTimeout(a.ctx, recentlyPlayedTimeout)
	defer cancelRecent()
	recent, _ := a.spotifyClient.RecentlyPlayedArtists(recentCtx)
	return artistsLoaded{artists, recent}
}

func (a artistModel) scheduleDiscography(artistID libspotify.ID) tea.Cmd {
	if _, ok := a.discographies[artistID]; ok {
		return nil
	}
	return a.after(songsLoadDelay, discographyRequested{artistID})
}

func (a artistModel) loadDiscography(artistID libspotify.ID) tea.Cmd {
	old, ok := a.discographies[artistID]
	if ok && old.state != stateDiscographyError {
		return nil
	}

	d := &artistDiscography{state: stateDiscographyLoading, loading: true, want: discographyPerPage}
	if ok {
		d.gen = old.gen + 1
	}
	a.discographies[artistID] = d
	return a.fetchDiscography(artistID, d.gen, 0)
}

func (a artistModel) refreshDiscography(artistID libspotify.ID) tea.Cmd {
	d, ok := a.discographies[artistID]
	if !ok || d.state == stateDiscographyError {
		return a.loadDiscography(artistID)
	}

	d.gen++
	d.loading, d.refreshing, d.buffer, d.err = true, true, nil, nil
	d.want = max(discographyPerPage, d.loaded)
	return a.fetchDiscography(artistID, d.gen, 0)
}

func (a artistModel) moreDiscography(artistID libspotify.ID) tea.Cmd {
	d := a.discographies[artistID]
	if d == nil || d.list == nil || d.loading || d.last || d.err != nil {
		return nil
	}
	if d.list.Index() < len(d.list.Items())-max(discographyPrefetch, d.list.Paginator.PerPage) {
		return nil
	}
	d.loading = true
	return a.fetchDiscography(artistID, d.gen, d.loaded)
}

func (a artistModel) fetchDiscography(artistID libspotify.ID, gen, offset int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, songsPageTimeout)
		defer cancel()

		page, err := a.spotifyClient.ArtistAlbumsPage(ctx, artistID, offset)
		if err != nil {
			return discographyLoadFailed{artistID, gen, err}
		}
		return discographyPageLoaded{artistID, gen, page}
	}
}

func (a artistModel) addDiscographyPage(msg discographyPageLoaded) (artistModel, tea.Cmd) {
	d, ok := a.discographies[msg.artistID]
	if !ok || msg.gen != d.gen || !d.loading {
		return a, nil
	}
	switch {
	case d.refreshing:
		d.buffer = append(d.buffer, unseenDiscography(d.buffer, msg.page.Items)...)
		if !msg.page.Last && msg.page.Next < d.want {
			return a, a.fetchDiscography(msg.artistID, msg.gen, msg.page.Next)
		}
		index := reselectDiscography(d.list, d.buffer)
		d.list = a.newDiscographyList(d.buffer)
		d.list.Select(index)
		d.buffer, d.refreshing = nil, false
	case d.list == nil:
		d.list = a.newDiscographyList(unseenDiscography(nil, msg.page.Items))
	default:
		unseen := unseenDiscography(listedDiscography(d.list), msg.page.Items)
		d.list.SetItems(append(d.list.Items(), discographyItems(unseen)...))
	}
	d.loaded, d.total, d.last = msg.page.Next, msg.page.Total, msg.page.Last
	d.state, d.loading = stateDiscographyLoaded, false

	return a, a.moreDiscography(msg.artistID)
}

func (a artistModel) failDiscography(msg discographyLoadFailed) {
	d, ok := a.discographies[msg.artistID]
	if !ok || msg.gen != d.gen || !d.loading {
		return
	}

	d.loading, d.refreshing, d.buffer, d.err = false, false, nil, msg.err
	d.state = stateDiscographyError
	if d.list != nil {
		d.state = stateDiscographyLoaded
	}
}

func (a artistModel) newDiscographyList(albums []spotify.Album) *list.Model {
	return a.newRightList(discographyItems(albums), discographyDelegate{focused: !a.leftFocus}, "album", "albums")
}

func (a artistModel) focusDiscographies() {
	for _, d := range a.discographies {
		if d.list != nil {
			d.list.SetDelegate(discographyDelegate{focused: !a.leftFocus})
		}
	}
}

func (a artistModel) resizeDiscographies() {
	for _, d := range a.discographies {
		if d.list != nil {
			a.resizeRightList(d.list)
		}
	}
}

func (a artistModel) play() (artistModel, tea.Cmd) {
	artist, ok := a.selectedArtist()
	if !ok {
		return a, nil
	}

	name, album := artist.title, libspotify.URI("")
	if !a.leftFocus {
		if selected, ok := a.selectedAlbum(); ok {
			name, album = discographyName(selected), selected.URI
		}
	}

	*a.plays++
	a.playback = playbackStatus{seq: a.playback.seq + 1, text: "Starting " + name + "...", starting: true}
	return a, a.startPlayback(a.playback.seq, name, artist.uri, album)
}

func (msg artistPlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Playing: true}
}

func (a artistModel) startPlayback(seq int, name string, artist, album libspotify.URI) tea.Cmd {
	var deviceID libspotify.ID
	if a.playerDevice != nil {
		deviceID = a.playerDevice.ID
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, playbackTimeout)
		defer cancel()

		var err error
		uri := artist
		if album == "" {
			err = a.spotifyClient.PlayArtist(ctx, deviceID, artist)
		} else {
			uri = album
			err = a.spotifyClient.PlayContext(ctx, deviceID, album, 0)
		}
		if err != nil {
			return artistPlaybackFailed{seq, err}
		}
		return artistPlaybackStarted{seq: seq, name: name, artist: artist, uri: uri}
	}
}

func (a artistModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, a.leftPane(a.list, a.msg, a.alignHorizontal), a.rightView()))
}

func (a artistModel) rightView() string {
	if a.list == nil {
		return ""
	}

	target := "albums"
	if !a.leftFocus {
		target = "artists"
	}
	keys := a.keyHelp(a.list.Help.Styles, target)
	if artist, ok := a.selectedArtist(); ok {
		return a.rightPane(artist.title, discographyProgress(a.discographies[artist.id]), func() string { return a.discographyView(artist) }, keys)
	}
	if len(a.list.Items()) == 0 {
		return a.rightPane("", "", func() string { return a.notice(noArtists) }, keys)
	}
	return ""
}

func (a artistModel) discographyView(artist artistItem) string {
	d := a.discographies[artist.id]
	switch {
	case d == nil:
		return a.notice("Loading albums...")
	case d.state == stateDiscographyError:
		return a.notice(fmt.Sprintf("Fail to load albums:\n%s\n\nPress r to try again.", errorText(d.err)))
	case d.list == nil:
		return a.notice("Loading albums...")
	case len(d.list.Items()) == 0:
		return a.notice(printable(artist.title) + " has no albums, singles or compilations.")
	}
	return d.list.View()
}

func discographyProgress(d *artistDiscography) string {
	switch {
	case d == nil:
		return ""
	case d.refreshing:
		return " (refreshing...)"
	case d.loading && d.list != nil:
		return fmt.Sprintf(" (loading %d of %d...)", d.loaded, d.total)
	case d.err != nil && d.list != nil:
		return " (fail to load: " + errorText(d.err) + ")"
	case d.list != nil && !d.last:
		return fmt.Sprintf(" (%d of %d)", d.loaded, d.total)
	}
	return ""
}
