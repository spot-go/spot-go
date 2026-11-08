package player

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	playlistModel struct {
		playerContext
		paneSize
		spinner         spinner.Model
		leftFocus       bool
		alignHorizontal lipgloss.Position
		msg             string
		state           playlistState
		list            *list.Model
		songs           map[libspotify.ID]*playlistSongs
		playback        playbackStatus
		after           func(time.Duration, tea.Msg) tea.Cmd
		sortPending     bool
		pollEvery       time.Duration
		polling         bool
		playingURI      libspotify.URI
	}

	item struct {
		title, desc, owner string
		id                 libspotify.ID
		uri                libspotify.URI
		index              int
	}

	playlistSongs struct {
		state      songsState
		err        error
		list       *list.Model
		gen        int
		loaded     int
		total      int
		loading    bool
		refreshing bool
		buffer     []spotify.PlaylistItem
	}

	playlistState int

	songsState int

	playlistsLoaded struct {
		playlists []libspotify.SimplePlaylist
		recent    []spotify.PlayedContext
	}

	playlistsLoadFailed struct {
		err error
	}

	songsRequested struct {
		playlistID libspotify.ID
	}

	songsPageLoaded struct {
		playlistID libspotify.ID
		gen        int
		page       spotify.PlaylistItemsPage
	}

	songsLoadFailed struct {
		playlistID libspotify.ID
		gen        int
		err        error
	}

	playbackStarted struct {
		seq  int
		name string
		uri  libspotify.URI
		item libspotify.URI
	}

	playbackFailed struct {
		seq int
		err error
	}

	pollPlayback struct{}

	playbackPolled struct {
		playing spotify.NowPlaying
		plays   int
		err     error
	}

	historySaveFailed struct {
		err error
	}
)

const (
	statePlaylistInit = playlistState(iota)
	statePlaylistError
	statePlaylistLoaded
)

const (
	stateSongsLoading = songsState(iota)
	stateSongsLoaded
	stateSongsError
	stateSongsForbidden
)

const (
	songsLoadDelay        = 150 * time.Millisecond
	songsPageTimeout      = 45 * time.Second
	playbackTimeout       = 15 * time.Second
	playbackPollInterval  = 30 * time.Second
	recentlyPlayedTimeout = 3 * time.Second
	pollSlack             = time.Second
)

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

func newPlaylistModel(ctx playerContext, historyErr error) playlistModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	p := playlistModel{
		playerContext:   ctx,
		spinner:         s,
		alignHorizontal: lipgloss.Center,
		leftFocus:       true,
		songs:           make(map[libspotify.ID]*playlistSongs),
		after:           after,
		pollEvery:       playbackPollInterval,
	}
	if historyErr != nil {
		p.playback = playbackStatus{text: "Fail to read the playlist history: " + errorText(historyErr), err: true}
	}
	return p
}

func (p playlistModel) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.playlists)
}

func (p playlistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m, cmd, handled := p.handleKey(msg); handled {
			return m, cmd
		}

	case contentDimension:
		p.paneSize = paneSize(msg)
		p.resizeSongs()

	case playlistsLoadFailed:
		p.state = statePlaylistError
		p.msg = fmt.Sprintf("Fail to load playlists:\n%s\n\nPress r to try again.", errorText(msg.err))
		p.list = nil
		p.alignHorizontal = lipgloss.Center
		if !p.polling && p.pollEvery > 0 {
			p.polling = true
			return p, p.fetchPlaying()
		}
		return p, nil

	case playlistsLoaded:
		p.state = statePlaylistLoaded

		items := make([]list.Item, 0, len(msg.playlists))
		uris := make([]libspotify.URI, 0, len(msg.playlists))
		for i, pl := range msg.playlists {
			title := pl.Name
			if title == "" {
				title = string(pl.ID)
			}
			owner := pl.Owner.DisplayName
			if owner == "" {
				owner = pl.Owner.ID
			}
			items = append(items, item{title: title, desc: pl.Description, owner: owner, id: pl.ID, uri: pl.URI, index: i})
			uris = append(uris, pl.URI)
		}
		p.history.Sync(uris, time.Now())
		for _, played := range msg.recent {
			p.history.Played(played.URI, played.At)
		}

		l := newLibraryList(sortItems(items, p.order(), *p.history), orderLabels[p.order()], "playlist", "playlists")
		p.alignHorizontal = lipgloss.Left

		p.list = &l

		cmds := []tea.Cmd{p.saveHistory()}
		if !p.polling && p.pollEvery > 0 {
			p.polling = true
			cmds = append(cmds, p.fetchPlaying())
		}
		if selected, ok := p.selectedPlaylist(); ok {
			cmds = append(cmds, p.loadSongs(selected.id))
		}
		return p, tea.Batch(cmds...)

	case songsRequested:
		if selected, ok := p.selectedPlaylist(); ok && selected.id == msg.playlistID {
			return p, p.loadSongs(msg.playlistID)
		}
		return p, nil

	case songsPageLoaded:
		return p, p.addSongsPage(msg)

	case songsLoadFailed:
		p.failSongs(msg)
		return p, nil

	case playbackStarted:
		if msg.seq == p.playback.seq {
			p.playback = playbackStatus{seq: msg.seq, text: "Playing: " + msg.name, item: msg.item, since: time.Now()}
		}
		p.playingURI = msg.uri
		return p.played(msg.uri, time.Now())

	case playbackFailed:
		if msg.seq == p.playback.seq {
			p.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		return p, nil

	case playbackToggled:
		if msg.plays == *p.plays {
			p.playback = p.playback.toggled(msg, time.Now())
		}
		return p, nil

	case pollPlayback:
		return p, p.fetchPlaying()

	case playbackPolled:
		var cmd tea.Cmd
		if msg.err == nil {
			if msg.plays == *p.plays {
				p.playback = p.playback.follow(msg.playing, time.Now())
			}
			if uri := msg.playing.Context; uri != p.playingURI {
				p.playingURI = uri
				if uri != "" {
					p, cmd = p.played(uri, time.Now())
				}
			}
		}
		if p.pollEvery > 0 {
			cmd = tea.Batch(cmd, p.after(nextPoll(p.pollEvery, msg.playing), pollPlayback{}))
		}
		return p, cmd

	case historySaveFailed:
		p.playback = playbackStatus{seq: p.playback.seq, text: "Fail to save the playlist history: " + errorText(msg.err), err: true}
		return p, nil
	}

	var cmd tea.Cmd
	switch p.state {
	case statePlaylistInit:
		p.spinner, cmd = p.spinner.Update(msg)
		p.msg = p.spinner.View()
		return p, cmd

	case statePlaylistLoaded:
		if _, isKey := msg.(tea.KeyPressMsg); isKey && !p.leftFocus {
			return p, p.updateSongs(msg)
		}
		return p.updatePlaylists(msg)
	}

	return p, nil
}

func (p playlistModel) handleKey(msg tea.KeyPressMsg) (playlistModel, tea.Cmd, bool) {
	if p.capturesInput() {
		return p, nil, false
	}

	switch msg.String() {
	case "tab":
		p.leftFocus = !p.leftFocus
		p.focusSongs()
		return p, nil, true

	case "r":
		if p.state == statePlaylistError {
			p.state = statePlaylistInit
			p.msg = p.spinner.View()
			return p, p.Init(), true
		}
		if selected, ok := p.selectedPlaylist(); ok {
			return p, p.refreshSongs(selected.id), true
		}

	case "enter":
		if p.state == statePlaylistLoaded {
			m, cmd := p.play()
			return m, cmd, true
		}

	case "s":
		if p.state == statePlaylistLoaded {
			p.history.SetOrder(nextOrder(playlistOrders, p.order()))
			p = p.sorted()
			return p, p.saveHistory(), true
		}
	}

	return p, nil, false
}

func (p playlistModel) updatePlaylists(msg tea.Msg) (tea.Model, tea.Cmd) {
	before, _ := p.selectedPlaylist()
	l, cmd := p.list.Update(msg)
	p.list = &l
	if p.sortPending {
		p = p.sorted()
	}

	current, ok := p.selectedPlaylist()
	if !ok || current.id == before.id {
		return p, cmd
	}
	return p, tea.Batch(cmd, p.scheduleSongs(current.id))
}

func (p playlistModel) updateSongs(msg tea.Msg) tea.Cmd {
	songs := p.selectedSongs()
	if songs == nil || songs.list == nil {
		return nil
	}
	l, cmd := songs.list.Update(msg)
	songs.list = &l
	return cmd
}

func (p playlistModel) capturesInput() bool {
	return p.list != nil && p.list.FilterState() == list.Filtering
}

func (p playlistModel) selectedPlaylist() (item, bool) {
	if p.list == nil {
		return item{}, false
	}
	i, ok := p.list.SelectedItem().(item)
	return i, ok
}

func (p playlistModel) order() library.Order {
	return validOrder(playlistOrders, p.history.Order)
}

func (p playlistModel) sorted() playlistModel {
	if p.list == nil {
		return p
	}
	p.list.Title = orderLabels[p.order()]
	if p.list.FilterState() == list.Filtering {
		p.sortPending = true
		return p
	}

	p.sortPending = false
	selected, ok := p.selectedPlaylist()
	p.list.SetItems(sortItems(p.list.Items(), p.order(), *p.history))
	if p.list.FilterState() == list.FilterApplied {
		p.list.SetFilterText(p.list.FilterValue())
	}
	if ok {
		p.selectPlaylist(selected.id)
	}
	return p
}

func (p playlistModel) selectPlaylist(id libspotify.ID) {
	for i, li := range p.list.VisibleItems() {
		if it, ok := li.(item); ok && it.id == id {
			p.list.Select(i)
			return
		}
	}
}

func (p playlistModel) played(uri libspotify.URI, at time.Time) (playlistModel, tea.Cmd) {
	if !p.history.Played(uri, at) {
		return p, nil
	}
	if p.order() == library.OrderRecents {
		p = p.sorted()
	}
	return p, p.saveHistory()
}

func (p playlistModel) saveHistory() tea.Cmd {
	store := p.historyStore
	if store == nil {
		return nil
	}
	h := p.history.Clone()
	return func() tea.Msg {
		if err := store.Save(h); err != nil {
			return historySaveFailed{err}
		}
		return nil
	}
}

func (p playlistModel) fetchPlaying() tea.Cmd {
	plays := *p.plays
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, playbackTimeout)
		defer cancel()

		playing, err := p.spotifyClient.NowPlaying(ctx)
		return playbackPolled{playing: playing, plays: plays, err: err}
	}
}

func nextPoll(every time.Duration, playing spotify.NowPlaying) time.Duration {
	left := playing.Duration - playing.Progress
	if left <= 0 {
		return every
	}
	return min(every, left+pollSlack)
}

func (p playlistModel) selectedSongs() *playlistSongs {
	selected, ok := p.selectedPlaylist()
	if !ok {
		return nil
	}
	return p.songs[selected.id]
}

func (p playlistModel) selectedSong() (spotify.PlaylistItem, bool) {
	songs := p.selectedSongs()
	if songs == nil || songs.list == nil {
		return spotify.PlaylistItem{}, false
	}
	s, ok := songs.list.SelectedItem().(songItem)
	return s.PlaylistItem, ok
}

func (p playlistModel) scheduleSongs(playlistID libspotify.ID) tea.Cmd {
	if _, ok := p.songs[playlistID]; ok {
		return nil
	}
	return p.after(songsLoadDelay, songsRequested{playlistID})
}

func (p playlistModel) loadSongs(playlistID libspotify.ID) tea.Cmd {
	old, ok := p.songs[playlistID]
	if ok && old.state != stateSongsError {
		return nil
	}

	songs := &playlistSongs{state: stateSongsLoading, loading: true}
	if ok {
		songs.gen = old.gen + 1
	}
	p.songs[playlistID] = songs
	return p.fetchSongs(playlistID, songs.gen, 0)
}

func (p playlistModel) refreshSongs(playlistID libspotify.ID) tea.Cmd {
	songs, ok := p.songs[playlistID]
	if !ok || songs.state == stateSongsError {
		return p.loadSongs(playlistID)
	}

	songs.gen++
	songs.loading, songs.refreshing, songs.buffer, songs.err = true, true, nil, nil
	return p.fetchSongs(playlistID, songs.gen, 0)
}

func (p playlistModel) fetchSongs(playlistID libspotify.ID, gen, offset int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, songsPageTimeout)
		defer cancel()

		page, err := p.spotifyClient.PlaylistItemsPage(ctx, playlistID, offset)
		if err != nil {
			return songsLoadFailed{playlistID, gen, err}
		}
		return songsPageLoaded{playlistID, gen, page}
	}
}

func (p playlistModel) addSongsPage(msg songsPageLoaded) tea.Cmd {
	songs, ok := p.songs[msg.playlistID]
	if !ok || msg.gen != songs.gen || !songs.loading {
		return nil
	}
	songs.loaded, songs.total = msg.page.Next, msg.page.Total

	switch {
	case songs.refreshing:
		songs.buffer = append(songs.buffer, msg.page.Items...)
		if msg.page.Last {
			index := reselect(songs.list, songs.buffer)
			songs.list = p.newSongsList(songs.buffer)
			songs.list.Select(index)
			songs.buffer = nil
		}
	case songs.list == nil:
		songs.list = p.newSongsList(msg.page.Items)
		songs.state = stateSongsLoaded
	default:
		songs.list.SetItems(append(songs.list.Items(), songItems(msg.page.Items)...))
	}

	if !msg.page.Last {
		return p.fetchSongs(msg.playlistID, msg.gen, msg.page.Next)
	}
	songs.state, songs.loading, songs.refreshing = stateSongsLoaded, false, false
	return nil
}

func (p playlistModel) failSongs(msg songsLoadFailed) {
	songs, ok := p.songs[msg.playlistID]
	if !ok || msg.gen != songs.gen || !songs.loading {
		return
	}

	songs.loading, songs.refreshing, songs.buffer, songs.err = false, false, nil, msg.err
	if songs.list != nil {
		songs.state = stateSongsLoaded
		return
	}
	songs.state = stateSongsError
	var apiErr libspotify.Error
	if errors.As(msg.err, &apiErr) && apiErr.Status == http.StatusForbidden {
		songs.state = stateSongsForbidden
	}
}

func reselect(previous *list.Model, songs []spotify.PlaylistItem) int {
	if previous == nil {
		return 0
	}
	index := min(previous.Index(), max(0, len(songs)-1))
	selected, ok := previous.SelectedItem().(songItem)
	if !ok {
		return index
	}

	distance := func(s spotify.PlaylistItem) int {
		d := s.Position - selected.Position
		if d < 0 {
			return -d
		}
		return d
	}
	best := -1
	for i, s := range songs {
		if s.URI == selected.URI && (best < 0 || distance(s) < distance(songs[best])) {
			best = i
		}
	}
	if best < 0 {
		return index
	}
	return best
}

func songItems(songs []spotify.PlaylistItem) []list.Item {
	items := make([]list.Item, len(songs))
	for i, s := range songs {
		items[i] = songItem{s}
	}
	return items
}

func (p playlistModel) newSongsList(songs []spotify.PlaylistItem) *list.Model {
	return p.newRightList(songItems(songs), songDelegate{focused: !p.leftFocus}, "song", "songs")
}

func (p playlistModel) focusSongs() {
	for _, songs := range p.songs {
		if songs.list != nil {
			songs.list.SetDelegate(songDelegate{focused: !p.leftFocus})
		}
	}
}

func (p playlistModel) resizeSongs() {
	for _, songs := range p.songs {
		if songs.list != nil {
			p.resizeRightList(songs.list)
		}
	}
}

func (p playlistModel) play() (playlistModel, tea.Cmd) {
	playlist, ok := p.selectedPlaylist()
	if !ok {
		return p, nil
	}

	position, name, item := 0, playlist.title, libspotify.URI("")
	if !p.leftFocus {
		if song, ok := p.selectedSong(); ok {
			if !song.Playable {
				p.playback = playbackStatus{seq: p.playback.seq + 1, text: unplayableReason(song), err: true}
				return p, nil
			}
			position, name, item = song.Position, song.Name, song.URI
		}
	}

	*p.plays++
	p.playback = playbackStatus{seq: p.playback.seq + 1, text: "Starting " + name + "...", starting: true}
	return p, p.startPlayback(p.playback.seq, playlist.uri, position, name, item)
}

func (p playlistModel) startPlayback(seq int, playlist libspotify.URI, position int, name string, item libspotify.URI) tea.Cmd {
	var deviceID libspotify.ID
	if p.playerDevice != nil {
		deviceID = p.playerDevice.ID
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, playbackTimeout)
		defer cancel()

		if err := p.spotifyClient.PlayContext(ctx, deviceID, playlist, position); err != nil {
			return playbackFailed{seq, err}
		}
		return playbackStarted{seq, name, playlist, item}
	}
}

func unplayableReason(song spotify.PlaylistItem) string {
	if song.IsLocal {
		return "Local files can't be played from spot-go"
	}
	return song.Name + " is unavailable"
}

func (p playlistModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, p.leftPane(p.list, p.msg, p.alignHorizontal), p.rightView()))
}

func (p playlistModel) rightView() string {
	playlist, ok := p.selectedPlaylist()
	if !ok {
		return ""
	}

	target := "songs"
	if !p.leftFocus {
		target = "playlists"
	}
	return p.rightPane(playlist.title, songsProgress(p.selectedSongs()), p.songsView, p.playback, p.keyHelp(p.list.Help.Styles, target))
}

func (p playlistModel) songsView() string {
	songs := p.selectedSongs()
	if songs == nil {
		return p.notice("Loading songs...")
	}

	switch songs.state {
	case stateSongsLoaded:
		if songs.list == nil {
			return ""
		}
		return songs.list.View()
	case stateSongsForbidden:
		return p.notice("Spotify only shows the songs of playlists you own or collaborate on.\n\nPress enter to play this playlist.")
	case stateSongsError:
		return p.notice(fmt.Sprintf("Fail to load songs:\n%s\n\nPress r to try again.", errorText(songs.err)))
	}
	return p.notice("Loading songs...")
}

func songsProgress(songs *playlistSongs) string {
	switch {
	case songs == nil:
		return ""
	case songs.refreshing:
		return " (refreshing...)"
	case songs.loading && songs.list != nil:
		return fmt.Sprintf(" (loading %d of %d...)", songs.loaded, songs.total)
	case songs.err != nil && songs.list != nil:
		return " (fail to load: " + errorText(songs.err) + ")"
	}
	return ""
}

func (p playlistModel) playlists() tea.Msg {
	playlists, err := p.spotifyClient.AllCurrentUsersPlaylists(
		p.ctx,
	)
	if err != nil {
		return playlistsLoadFailed{err}
	}

	ctx, cancel := context.WithTimeout(p.ctx, recentlyPlayedTimeout)
	defer cancel()
	recent, _ := p.spotifyClient.RecentlyPlayedPlaylists(ctx)
	return playlistsLoaded{playlists, recent}
}
