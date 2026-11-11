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
	albumModel struct {
		playerContext
		paneSize
		spinner         spinner.Model
		leftFocus       bool
		alignHorizontal lipgloss.Position
		msg             string
		state           savedAlbumsState
		list            *list.Model
		songs           map[libspotify.ID]*playlistSongs
		playback        playbackStatus
		volume          volumeLevel
		sortPending     bool
		playingURI      libspotify.URI
		after           func(time.Duration, tea.Msg) tea.Cmd
	}

	albumItem struct {
		title   string
		artist  string
		id      libspotify.ID
		uri     libspotify.URI
		addedAt time.Time
		index   int
	}

	savedAlbumsState int

	albumsLoaded struct {
		albums []spotify.SavedAlbum
		recent []spotify.PlayedContext
	}

	albumsLoadFailed struct {
		err error
	}

	albumSongsRequested struct {
		albumID libspotify.ID
	}

	albumSongsPageLoaded struct {
		albumID libspotify.ID
		gen     int
		page    spotify.PlaylistItemsPage
	}

	albumSongsLoadFailed struct {
		albumID libspotify.ID
		gen     int
		err     error
	}

	albumPlaybackStarted struct {
		seq      int
		name     string
		artist   string
		duration time.Duration
		uri      libspotify.URI
		item     libspotify.URI
	}

	albumPlaybackFailed struct {
		seq int
		err error
	}

	albumHistorySaveFailed struct {
		err error
	}
)

const (
	stateSavedAlbumsInit = savedAlbumsState(iota)
	stateSavedAlbumsError
	stateSavedAlbumsLoaded
)

const (
	savedAlbumsTimeout = 45 * time.Second
	albumContextPrefix = "spotify:album:"
	noSavedAlbums      = "You haven't saved any albums.\n\nSave albums in the Spotify app, then press r."
	noAlbumSongs       = "No songs in this album."
)

func (a albumItem) Title() string       { return a.title }
func (a albumItem) Description() string { return "" }
func (a albumItem) FilterValue() string { return a.title }

func (a albumItem) heading() string {
	if a.artist == "" {
		return a.title
	}
	return a.title + " - " + a.artist
}

func newAlbumModel(ctx playerContext, historyErr error) albumModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	a := albumModel{
		playerContext:   ctx,
		spinner:         s,
		alignHorizontal: lipgloss.Center,
		leftFocus:       true,
		msg:             s.View(),
		songs:           make(map[libspotify.ID]*playlistSongs),
		after:           after,
	}
	if historyErr != nil {
		a.playback = playbackStatus{text: "Fail to read the album history: " + errorText(historyErr), err: true}
	}
	return a
}

func (a albumModel) Init() tea.Cmd {
	return tea.Batch(a.spinner.Tick, a.albums)
}

func (a albumModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m, cmd, handled := a.handleKey(msg); handled {
			return m, cmd
		}

	case contentDimension:
		a.paneSize = paneSize(msg)
		a.resizeSongs()

	case albumsLoadFailed:
		a.state = stateSavedAlbumsError
		a.msg = fmt.Sprintf("Fail to load albums:\n%s\n\nPress r to try again.", errorText(msg.err))
		a.list = nil
		a.alignHorizontal = lipgloss.Center
		return a, nil

	case albumsLoaded:
		return a.loaded(msg)

	case albumSongsRequested:
		if selected, ok := a.selectedAlbum(); ok && selected.id == msg.albumID {
			return a, a.loadSongs(msg.albumID)
		}
		return a, nil

	case albumSongsPageLoaded:
		return a, a.addSongsPage(msg)

	case albumSongsLoadFailed:
		a.failSongs(msg)
		return a, nil

	case albumPlaybackStarted:
		if msg.seq == a.playback.seq {
			a.playback = playingStatus(msg.seq, msg.item, msg.info(), time.Now())
		}
		a.playingURI = msg.uri
		return a.played(msg.uri, time.Now())

	case artistPlaybackStarted:
		if !strings.HasPrefix(string(msg.uri), albumContextPrefix) {
			return a, nil
		}
		a.playingURI = msg.uri
		return a.played(msg.uri, time.Now())

	case albumPlaybackFailed:
		if msg.seq == a.playback.seq {
			a.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		return a, nil

	case playbackToggled:
		if msg.plays == *a.plays {
			a.playback = a.playback.toggled(msg, time.Now())
		}
		return a, nil

	case volumeChanged:
		a.volume = a.volume.changed(msg)
		return a, nil

	case volumeHidden:
		a.volume = a.volume.hidden(msg)
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
		if strings.HasPrefix(string(uri), albumContextPrefix) {
			return a.played(uri, time.Now())
		}
		return a, nil

	case albumHistorySaveFailed:
		a.playback = playbackStatus{seq: a.playback.seq, text: "Fail to save the album history: " + errorText(msg.err), err: true}
		return a, nil
	}

	var cmd tea.Cmd
	switch a.state {
	case stateSavedAlbumsInit:
		a.spinner, cmd = a.spinner.Update(msg)
		a.msg = a.spinner.View()
		return a, cmd

	case stateSavedAlbumsLoaded:
		if _, isKey := msg.(tea.KeyPressMsg); isKey && !a.leftFocus {
			return a, a.updateSongs(msg)
		}
		return a.updateAlbums(msg)
	}

	return a, nil
}

func (a albumModel) loaded(msg albumsLoaded) (tea.Model, tea.Cmd) {
	a.state = stateSavedAlbumsLoaded

	items := make([]list.Item, 0, len(msg.albums))
	uris := make([]libspotify.URI, 0, len(msg.albums))
	for i, al := range msg.albums {
		title := al.Name
		if title == "" {
			title = string(al.ID)
		}
		items = append(items, albumItem{
			title:   title,
			artist:  strings.Join(al.Artists, ", "),
			id:      al.ID,
			uri:     al.URI,
			addedAt: al.AddedAt,
			index:   i,
		})
		uris = append(uris, al.URI)
	}
	for _, played := range msg.recent {
		a.history.AlbumPlayed(played.URI, played.At)
	}
	a.history.SyncAlbums(uris)

	l := newLibraryList(sortAlbums(items, a.order(), *a.history), orderLabels[a.order()], "album", "albums")
	a.list = &l
	a.alignHorizontal = lipgloss.Left

	cmds := []tea.Cmd{a.saveHistory()}
	if selected, ok := a.selectedAlbum(); ok {
		cmds = append(cmds, a.loadSongs(selected.id))
	}
	return a, tea.Batch(cmds...)
}

func (a albumModel) handleKey(msg tea.KeyPressMsg) (albumModel, tea.Cmd, bool) {
	if a.capturesInput() {
		return a, nil, false
	}

	switch msg.String() {
	case "tab":
		a.leftFocus = !a.leftFocus
		a.focusSongs()
		return a, nil, true

	case "r":
		if a.state == stateSavedAlbumsError || (a.state == stateSavedAlbumsLoaded && len(a.list.Items()) == 0) {
			a.state = stateSavedAlbumsInit
			a.list = nil
			a.alignHorizontal = lipgloss.Center
			a.msg = a.spinner.View()
			return a, a.Init(), true
		}
		if selected, ok := a.selectedAlbum(); ok {
			return a, a.refreshSongs(selected.id), true
		}

	case "enter":
		if a.state == stateSavedAlbumsLoaded {
			m, cmd := a.play()
			return m, cmd, true
		}

	case "s":
		if a.state == stateSavedAlbumsLoaded {
			a.history.SetAlbumOrder(nextOrder(albumOrders, a.order()))
			a = a.sorted()
			return a, a.saveHistory(), true
		}
	}

	return a, nil, false
}

func (a albumModel) updateAlbums(msg tea.Msg) (tea.Model, tea.Cmd) {
	before, _ := a.selectedAlbum()
	l, cmd := a.list.Update(msg)
	a.list = &l
	if a.sortPending {
		a = a.sorted()
	}

	current, ok := a.selectedAlbum()
	if !ok || current.id == before.id {
		return a, cmd
	}
	return a, tea.Batch(cmd, a.scheduleSongs(current.id))
}

func (a albumModel) updateSongs(msg tea.Msg) tea.Cmd {
	songs := a.selectedSongs()
	if songs == nil || songs.list == nil {
		return nil
	}
	l, cmd := songs.list.Update(msg)
	songs.list = &l
	return cmd
}

func (a albumModel) capturesInput() bool {
	return a.list != nil && a.list.FilterState() == list.Filtering
}

func (a albumModel) status() playbackStatus {
	return a.playback
}

func (a albumModel) selectedAlbum() (albumItem, bool) {
	if a.list == nil {
		return albumItem{}, false
	}
	al, ok := a.list.SelectedItem().(albumItem)
	return al, ok
}

func (a albumModel) selectedSongs() *playlistSongs {
	album, ok := a.selectedAlbum()
	if !ok {
		return nil
	}
	return a.songs[album.id]
}

func (a albumModel) selectedSong() (spotify.PlaylistItem, bool) {
	songs := a.selectedSongs()
	if songs == nil || songs.list == nil {
		return spotify.PlaylistItem{}, false
	}
	s, ok := songs.list.SelectedItem().(songItem)
	return s.PlaylistItem, ok
}

func (a albumModel) order() library.Order {
	return validOrder(albumOrders, a.history.Albums.Order)
}

func (a albumModel) sorted() albumModel {
	if a.list == nil {
		return a
	}
	a.list.Title = orderLabels[a.order()]
	if a.list.FilterState() == list.Filtering {
		a.sortPending = true
		return a
	}

	a.sortPending = false
	selected, ok := a.selectedAlbum()
	a.list.SetItems(sortAlbums(a.list.Items(), a.order(), *a.history))
	if a.list.FilterState() == list.FilterApplied {
		a.list.SetFilterText(a.list.FilterValue())
	}
	if ok {
		a.selectAlbum(selected.id)
	}
	return a
}

func (a albumModel) selectAlbum(id libspotify.ID) {
	for i, li := range a.list.VisibleItems() {
		if al, ok := li.(albumItem); ok && al.id == id {
			a.list.Select(i)
			return
		}
	}
}

func (a albumModel) played(uri libspotify.URI, at time.Time) (albumModel, tea.Cmd) {
	if !a.history.AlbumPlayed(uri, at) {
		return a, nil
	}
	if a.order() == library.OrderRecents {
		a = a.sorted()
	}
	return a, a.saveHistory()
}

func (a albumModel) saveHistory() tea.Cmd {
	store := a.historyStore
	if store == nil {
		return nil
	}
	h := a.history.Clone()
	return func() tea.Msg {
		if err := store.Save(h); err != nil {
			return albumHistorySaveFailed{err}
		}
		return nil
	}
}

func (a albumModel) albums() tea.Msg {
	ctx, cancel := context.WithTimeout(a.ctx, savedAlbumsTimeout)
	defer cancel()

	albums, err := a.spotifyClient.AllSavedAlbums(ctx)
	if err != nil {
		return albumsLoadFailed{err}
	}

	recentCtx, cancelRecent := context.WithTimeout(a.ctx, recentlyPlayedTimeout)
	defer cancelRecent()
	recent, _ := a.spotifyClient.RecentlyPlayedAlbums(recentCtx)
	return albumsLoaded{albums, recent}
}

func (a albumModel) scheduleSongs(albumID libspotify.ID) tea.Cmd {
	if _, ok := a.songs[albumID]; ok {
		return nil
	}
	return a.after(songsLoadDelay, albumSongsRequested{albumID})
}

func (a albumModel) loadSongs(albumID libspotify.ID) tea.Cmd {
	old, ok := a.songs[albumID]
	if ok && old.state != stateSongsError {
		return nil
	}

	songs := &playlistSongs{state: stateSongsLoading, loading: true}
	if ok {
		songs.gen = old.gen + 1
	}
	a.songs[albumID] = songs
	return a.fetchSongs(albumID, songs.gen, 0)
}

func (a albumModel) refreshSongs(albumID libspotify.ID) tea.Cmd {
	songs, ok := a.songs[albumID]
	if !ok || songs.state == stateSongsError {
		return a.loadSongs(albumID)
	}

	songs.gen++
	songs.loading, songs.refreshing, songs.buffer, songs.err = true, true, nil, nil
	return a.fetchSongs(albumID, songs.gen, 0)
}

func (a albumModel) fetchSongs(albumID libspotify.ID, gen, offset int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, songsPageTimeout)
		defer cancel()

		page, err := a.spotifyClient.AlbumTracksPage(ctx, albumID, offset)
		if err != nil {
			return albumSongsLoadFailed{albumID, gen, err}
		}
		return albumSongsPageLoaded{albumID, gen, page}
	}
}

func (a albumModel) addSongsPage(msg albumSongsPageLoaded) tea.Cmd {
	songs, ok := a.songs[msg.albumID]
	if !ok || msg.gen != songs.gen || !songs.loading {
		return nil
	}
	songs.loaded, songs.total = msg.page.Next, msg.page.Total

	switch {
	case songs.refreshing:
		songs.buffer = append(songs.buffer, msg.page.Items...)
		if msg.page.Last {
			index := reselect(songs.list, songs.buffer)
			songs.list = a.newSongsList(songs.buffer)
			songs.list.Select(index)
			songs.buffer = nil
		}
	case songs.list == nil:
		songs.list = a.newSongsList(msg.page.Items)
		songs.state = stateSongsLoaded
	default:
		songs.list.SetItems(append(songs.list.Items(), songItems(msg.page.Items)...))
	}

	if !msg.page.Last {
		return a.fetchSongs(msg.albumID, msg.gen, msg.page.Next)
	}
	songs.state, songs.loading, songs.refreshing = stateSongsLoaded, false, false
	return nil
}

func (a albumModel) failSongs(msg albumSongsLoadFailed) {
	songs, ok := a.songs[msg.albumID]
	if !ok || msg.gen != songs.gen || !songs.loading {
		return
	}

	songs.loading, songs.refreshing, songs.buffer, songs.err = false, false, nil, msg.err
	songs.state = stateSongsError
	if songs.list != nil {
		songs.state = stateSongsLoaded
	}
}

func (a albumModel) newSongsList(songs []spotify.PlaylistItem) *list.Model {
	return a.newRightList(songItems(songs), songDelegate{focused: !a.leftFocus}, "song", "songs")
}

func (a albumModel) focusSongs() {
	for _, songs := range a.songs {
		if songs.list != nil {
			songs.list.SetDelegate(songDelegate{focused: !a.leftFocus})
		}
	}
}

func (a albumModel) resizeSongs() {
	for _, songs := range a.songs {
		if songs.list != nil {
			a.resizeRightList(songs.list)
		}
	}
}

func (a albumModel) play() (albumModel, tea.Cmd) {
	album, ok := a.selectedAlbum()
	if !ok {
		return a, nil
	}

	position, started := 0, albumPlaybackStarted{name: album.title, uri: album.uri}
	if !a.leftFocus {
		if song, ok := a.selectedSong(); ok {
			if !song.Playable {
				a.playback = playbackStatus{seq: a.playback.seq + 1, text: unplayableReason(song), err: true}
				return a, nil
			}
			position = song.Position
			started.name, started.artist, started.duration, started.item = song.Name, song.Artist(), song.Duration, song.URI
		}
	}

	*a.plays++
	a.playback = playbackStatus{seq: a.playback.seq + 1, text: "Starting " + started.name + "...", starting: true}
	started.seq = a.playback.seq
	return a, a.startPlayback(started, position)
}

func (a albumModel) startPlayback(started albumPlaybackStarted, position int) tea.Cmd {
	var deviceID libspotify.ID
	if a.playerDevice != nil {
		deviceID = a.playerDevice.ID
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.ctx, playbackTimeout)
		defer cancel()

		if err := a.spotifyClient.PlayContext(ctx, deviceID, started.uri, position); err != nil {
			return albumPlaybackFailed{started.seq, err}
		}
		return started
	}
}

func (msg albumPlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Artist: msg.artist, Duration: msg.duration, Playing: true}
}

func (a albumModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, a.leftPane(a.list, a.msg, a.alignHorizontal), a.rightView()))
}

func (a albumModel) rightView() string {
	if a.list == nil {
		return ""
	}

	target := "songs"
	if !a.leftFocus {
		target = "albums"
	}
	keys := a.keyHelp(a.list.Help.Styles, target)
	if album, ok := a.selectedAlbum(); ok {
		return a.rightPane(album.heading(), songsProgress(a.songs[album.id]), a.songsView, a.playback, a.volume, keys)
	}
	if len(a.list.Items()) == 0 {
		return a.rightPane("", "", func() string { return a.notice(noSavedAlbums) }, a.playback, a.volume, keys)
	}
	return ""
}

func (a albumModel) songsView() string {
	songs := a.selectedSongs()
	switch {
	case songs == nil:
		return a.notice("Loading songs...")
	case songs.state == stateSongsError:
		return a.notice(fmt.Sprintf("Fail to load songs:\n%s\n\nPress r to try again.", errorText(songs.err)))
	case songs.list == nil:
		return a.notice("Loading songs...")
	case len(songs.list.Items()) == 0:
		return a.notice(noAlbumSongs)
	}
	return songs.list.View()
}
