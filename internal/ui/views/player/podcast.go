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
	"github.com/franciscocpg/spot-go/internal/library"
	"github.com/franciscocpg/spot-go/internal/media"
	"github.com/franciscocpg/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	podcastModel struct {
		playerContext
		paneSize
		spinner         spinner.Model
		leftFocus       bool
		alignHorizontal lipgloss.Position
		msg             string
		state           podcastState
		list            *list.Model
		episodes        map[libspotify.ID]*showEpisodes
		playback        playbackStatus
		pending         pendingPlay
		sortPending     bool
		playingURI      libspotify.URI
		after           func(time.Duration, tea.Msg) tea.Cmd
	}

	showItem struct {
		title   string
		id      libspotify.ID
		uri     libspotify.URI
		addedAt time.Time
		index   int
	}

	showEpisodes struct {
		state      episodesState
		err        error
		list       *list.Model
		gen        int
		loaded     int
		total      int
		want       int
		last       bool
		loading    bool
		refreshing bool
		buffer     []spotify.Episode
	}

	pendingPlay struct {
		seq  int
		play int
		show libspotify.ID
	}

	podcastState int

	episodesState int

	showsLoaded struct {
		shows []spotify.SavedShow
	}

	showsLoadFailed struct {
		err error
	}

	episodesRequested struct {
		showID libspotify.ID
	}

	episodesPageLoaded struct {
		showID libspotify.ID
		gen    int
		page   spotify.EpisodesPage
	}

	episodesLoadFailed struct {
		showID libspotify.ID
		gen    int
		err    error
	}

	episodePlaybackStarted struct {
		seq      int
		show     libspotify.URI
		episode  libspotify.URI
		name     string
		showName string
		from     time.Duration
		duration time.Duration
	}

	episodePlaybackFailed struct {
		seq int
		err error
	}

	podcastHistorySaveFailed struct {
		err error
	}
)

const (
	statePodcastsInit = podcastState(iota)
	statePodcastsError
	statePodcastsLoaded
)

const (
	stateEpisodesLoading = episodesState(iota)
	stateEpisodesLoaded
	stateEpisodesError
)

const (
	podcastsTimeout    = 45 * time.Second
	episodeReadTimeout = 3 * time.Second
	episodesPerPage    = 50
	episodesPrefetch   = 10
	noPodcasts         = "You don't follow any podcasts.\n\nFollow shows in the Spotify app, then press r."
)

func (s showItem) Title() string       { return s.title }
func (s showItem) Description() string { return "" }
func (s showItem) FilterValue() string { return s.title }

func newPodcastModel(ctx playerContext, historyErr error) podcastModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	p := podcastModel{
		playerContext:   ctx,
		spinner:         s,
		alignHorizontal: lipgloss.Center,
		leftFocus:       true,
		msg:             s.View(),
		episodes:        make(map[libspotify.ID]*showEpisodes),
		after:           after,
	}
	if historyErr != nil {
		p.playback = playbackStatus{text: "Fail to read the podcast history: " + errorText(historyErr), err: true}
	}
	return p
}

func (p podcastModel) Init() tea.Cmd {
	return tea.Batch(p.spinner.Tick, p.shows)
}

func (p podcastModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m, cmd, handled := p.handleKey(msg); handled {
			return m, cmd
		}

	case contentDimension:
		p.paneSize = paneSize(msg)
		p.resizeEpisodes()

	case showsLoadFailed:
		p.state = statePodcastsError
		p.msg = fmt.Sprintf("Fail to load podcasts:\n%s\n\nPress r to try again.", errorText(msg.err))
		p.list = nil
		p.alignHorizontal = lipgloss.Center
		return p, nil

	case showsLoaded:
		return p.loaded(msg.shows)

	case episodesRequested:
		if selected, ok := p.selectedShow(); ok && selected.id == msg.showID {
			return p, p.loadEpisodes(msg.showID)
		}
		return p, nil

	case episodesPageLoaded:
		return p.addEpisodesPage(msg)

	case episodesLoadFailed:
		return p.failEpisodes(msg), nil

	case episodePlaybackStarted:
		if msg.seq == p.playback.seq {
			p.playback = playingStatus(msg.seq, msg.episode, msg.info(), time.Now())
		}
		p.playingURI = msg.show
		return p.played(msg.show, time.Now())

	case episodePlaybackFailed:
		if msg.seq == p.playback.seq {
			p.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		return p, nil

	case playbackToggled:
		if msg.plays == *p.plays {
			p.playback = p.playback.toggled(msg, time.Now())
		}
		return p, nil

	case playbackConfirmed:
		p.playback = p.playback.followNow(msg.playing, time.Now())
		return p, nil

	case playbackPolled:
		if msg.err != nil {
			return p, nil
		}
		if msg.plays == *p.plays {
			p.playback = p.playback.follow(msg.playing, time.Now())
		}
		uri := msg.playing.Context
		if uri == p.playingURI {
			return p, nil
		}
		p.playingURI = uri
		if strings.HasPrefix(string(uri), "spotify:show:") {
			return p.played(uri, time.Now())
		}
		return p, nil

	case podcastHistorySaveFailed:
		p.playback = playbackStatus{seq: p.playback.seq, text: "Fail to save the podcast history: " + errorText(msg.err), err: true}
		return p, nil
	}

	var cmd tea.Cmd
	switch p.state {
	case statePodcastsInit:
		p.spinner, cmd = p.spinner.Update(msg)
		p.msg = p.spinner.View()
		return p, cmd

	case statePodcastsLoaded:
		if _, isKey := msg.(tea.KeyPressMsg); isKey && !p.leftFocus {
			return p.updateEpisodes(msg)
		}
		return p.updateShows(msg)
	}

	return p, nil
}

func (p podcastModel) loaded(shows []spotify.SavedShow) (tea.Model, tea.Cmd) {
	p.state = statePodcastsLoaded

	items := make([]list.Item, 0, len(shows))
	uris := make([]libspotify.URI, 0, len(shows))
	for i, s := range shows {
		title := s.Name
		if title == "" {
			title = string(s.ID)
		}
		items = append(items, showItem{title: title, id: s.ID, uri: s.URI, addedAt: s.AddedAt, index: i})
		uris = append(uris, s.URI)
	}
	p.history.SyncPodcasts(uris)

	l := newLibraryList(sortShows(items, p.order(), *p.history), orderLabels[p.order()], "podcast", "podcasts")
	p.list = &l
	p.alignHorizontal = lipgloss.Left

	cmds := []tea.Cmd{p.saveHistory()}
	if selected, ok := p.selectedShow(); ok {
		cmds = append(cmds, p.loadEpisodes(selected.id))
	}
	return p, tea.Batch(cmds...)
}

func (p podcastModel) handleKey(msg tea.KeyPressMsg) (podcastModel, tea.Cmd, bool) {
	if p.capturesInput() {
		return p, nil, false
	}

	switch msg.String() {
	case "tab":
		p.leftFocus = !p.leftFocus
		p.focusEpisodes()
		return p, nil, true

	case "r":
		if p.state == statePodcastsError || (p.state == statePodcastsLoaded && len(p.list.Items()) == 0) {
			p.state = statePodcastsInit
			p.list = nil
			p.alignHorizontal = lipgloss.Center
			p.msg = p.spinner.View()
			return p, p.Init(), true
		}
		if selected, ok := p.selectedShow(); ok {
			return p, p.refreshEpisodes(selected.id), true
		}

	case "enter":
		if p.state == statePodcastsLoaded {
			m, cmd := p.play()
			return m, cmd, true
		}

	case "s":
		if p.state == statePodcastsLoaded {
			p.history.SetPodcastOrder(nextOrder(podcastOrders, p.order()))
			p = p.sorted()
			return p, p.saveHistory(), true
		}
	}

	return p, nil, false
}

func (p podcastModel) updateShows(msg tea.Msg) (tea.Model, tea.Cmd) {
	before, _ := p.selectedShow()
	l, cmd := p.list.Update(msg)
	p.list = &l
	if p.sortPending {
		p = p.sorted()
	}

	current, ok := p.selectedShow()
	if !ok || current.id == before.id {
		return p, cmd
	}
	return p, tea.Batch(cmd, p.scheduleEpisodes(current.id))
}

func (p podcastModel) updateEpisodes(msg tea.Msg) (tea.Model, tea.Cmd) {
	show, ok := p.selectedShow()
	if !ok {
		return p, nil
	}
	e := p.episodes[show.id]
	if e == nil || e.list == nil {
		return p, nil
	}
	l, cmd := e.list.Update(msg)
	e.list = &l
	return p, tea.Batch(cmd, p.moreEpisodes(show.id))
}

func (p podcastModel) capturesInput() bool {
	return p.list != nil && p.list.FilterState() == list.Filtering
}

func (p podcastModel) status() playbackStatus {
	return p.playback
}

func (p podcastModel) selectedShow() (showItem, bool) {
	if p.list == nil {
		return showItem{}, false
	}
	s, ok := p.list.SelectedItem().(showItem)
	return s, ok
}

func (p podcastModel) findShow(id libspotify.ID) (showItem, bool) {
	if p.list == nil {
		return showItem{}, false
	}
	for _, li := range p.list.Items() {
		if s, ok := li.(showItem); ok && s.id == id {
			return s, true
		}
	}
	return showItem{}, false
}

func (p podcastModel) selectedEpisodes() *showEpisodes {
	show, ok := p.selectedShow()
	if !ok {
		return nil
	}
	return p.episodes[show.id]
}

func (p podcastModel) selectedEpisode() (spotify.Episode, bool) {
	e := p.selectedEpisodes()
	if e == nil || e.list == nil {
		return spotify.Episode{}, false
	}
	ep, ok := e.list.SelectedItem().(episodeItem)
	return ep.Episode, ok
}

func (p podcastModel) order() library.Order {
	return validOrder(podcastOrders, p.history.Podcasts.Order)
}

func (p podcastModel) sorted() podcastModel {
	if p.list == nil {
		return p
	}
	p.list.Title = orderLabels[p.order()]
	if p.list.FilterState() == list.Filtering {
		p.sortPending = true
		return p
	}

	p.sortPending = false
	selected, ok := p.selectedShow()
	p.list.SetItems(sortShows(p.list.Items(), p.order(), *p.history))
	if p.list.FilterState() == list.FilterApplied {
		p.list.SetFilterText(p.list.FilterValue())
	}
	if ok {
		p.selectShow(selected.id)
	}
	return p
}

func (p podcastModel) selectShow(id libspotify.ID) {
	for i, li := range p.list.VisibleItems() {
		if s, ok := li.(showItem); ok && s.id == id {
			p.list.Select(i)
			return
		}
	}
}

func (p podcastModel) played(uri libspotify.URI, at time.Time) (podcastModel, tea.Cmd) {
	if !p.history.PodcastPlayed(uri, at) {
		return p, nil
	}
	if p.order() == library.OrderRecents {
		p = p.sorted()
	}
	return p, p.saveHistory()
}

func (p podcastModel) saveHistory() tea.Cmd {
	store := p.historyStore
	if store == nil {
		return nil
	}
	h := p.history.Clone()
	return func() tea.Msg {
		if err := store.Save(h); err != nil {
			return podcastHistorySaveFailed{err}
		}
		return nil
	}
}

func (p podcastModel) shows() tea.Msg {
	ctx, cancel := context.WithTimeout(p.ctx, podcastsTimeout)
	defer cancel()

	shows, err := p.spotifyClient.AllSavedShows(ctx)
	if err != nil {
		return showsLoadFailed{err}
	}
	return showsLoaded{shows}
}

func (p podcastModel) scheduleEpisodes(showID libspotify.ID) tea.Cmd {
	if _, ok := p.episodes[showID]; ok {
		return nil
	}
	return p.after(songsLoadDelay, episodesRequested{showID})
}

func (p podcastModel) loadEpisodes(showID libspotify.ID) tea.Cmd {
	old, ok := p.episodes[showID]
	if ok && old.state != stateEpisodesError {
		return nil
	}

	e := &showEpisodes{state: stateEpisodesLoading, loading: true, want: episodesPerPage}
	if ok {
		e.gen = old.gen + 1
	}
	p.episodes[showID] = e
	return p.fetchEpisodes(showID, e.gen, 0)
}

func (p podcastModel) refreshEpisodes(showID libspotify.ID) tea.Cmd {
	e, ok := p.episodes[showID]
	if !ok || e.state == stateEpisodesError {
		return p.loadEpisodes(showID)
	}

	e.gen++
	e.loading, e.refreshing, e.buffer, e.err = true, true, nil, nil
	e.want = max(episodesPerPage, e.loaded)
	return p.fetchEpisodes(showID, e.gen, 0)
}

func (p podcastModel) moreEpisodes(showID libspotify.ID) tea.Cmd {
	e := p.episodes[showID]
	if e == nil || e.list == nil || e.loading || e.last || e.err != nil {
		return nil
	}
	if e.list.Index() < len(e.list.Items())-episodesPrefetch {
		return nil
	}
	e.loading = true
	return p.fetchEpisodes(showID, e.gen, e.loaded)
}

func (p podcastModel) fetchEpisodes(showID libspotify.ID, gen, offset int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, songsPageTimeout)
		defer cancel()

		page, err := p.spotifyClient.ShowEpisodesPage(ctx, showID, offset)
		if err != nil {
			return episodesLoadFailed{showID, gen, err}
		}
		return episodesPageLoaded{showID, gen, page}
	}
}

func (p podcastModel) addEpisodesPage(msg episodesPageLoaded) (podcastModel, tea.Cmd) {
	e, ok := p.episodes[msg.showID]
	if !ok || msg.gen != e.gen || !e.loading {
		return p, nil
	}
	switch {
	case e.refreshing:
		e.buffer = append(e.buffer, unseenEpisodes(e.buffer, msg.page.Items)...)
		if !msg.page.Last && msg.page.Next < e.want {
			return p, p.fetchEpisodes(msg.showID, msg.gen, msg.page.Next)
		}
		index := reselectEpisode(e.list, e.buffer)
		e.list = p.newEpisodesList(e.buffer)
		e.list.Select(index)
		e.buffer, e.refreshing = nil, false
	case e.list == nil:
		e.list = p.newEpisodesList(unseenEpisodes(nil, msg.page.Items))
	default:
		unseen := unseenEpisodes(listedEpisodes(e.list), msg.page.Items)
		e.list.SetItems(append(e.list.Items(), episodeItems(unseen)...))
	}
	e.loaded, e.total, e.last = msg.page.Next, msg.page.Total, msg.page.Last
	e.state, e.loading = stateEpisodesLoaded, false

	p, cmd := p.playPending(msg.showID)
	return p, tea.Batch(cmd, p.moreEpisodes(msg.showID))
}

func (p podcastModel) failEpisodes(msg episodesLoadFailed) podcastModel {
	e, ok := p.episodes[msg.showID]
	if !ok || msg.gen != e.gen || !e.loading {
		return p
	}

	e.loading, e.refreshing, e.buffer, e.err = false, false, nil, msg.err
	e.state = stateEpisodesError
	if e.list != nil {
		e.state = stateEpisodesLoaded
	}
	if p.pending.show == msg.showID {
		if p.pending.seq == p.playback.seq {
			p.playback = playbackStatus{seq: p.playback.seq}
			if p.pending.play == *p.plays {
				p.playback = playbackStatus{seq: p.playback.seq, text: "Fail to play: " + errorText(msg.err), err: true}
			}
		}
		p.pending = pendingPlay{}
	}
	return p
}

func reselectEpisode(previous *list.Model, episodes []spotify.Episode) int {
	if previous == nil {
		return 0
	}
	index := min(previous.Index(), max(0, len(episodes)-1))
	selected, ok := previous.SelectedItem().(episodeItem)
	if !ok {
		return index
	}
	for i, e := range episodes {
		if e.URI == selected.URI {
			return i
		}
	}
	return index
}

func (p podcastModel) newEpisodesList(episodes []spotify.Episode) *list.Model {
	return p.newRightList(episodeItems(episodes), episodeDelegate{focused: !p.leftFocus}, "episode", "episodes")
}

func (p podcastModel) focusEpisodes() {
	for _, e := range p.episodes {
		if e.list != nil {
			e.list.SetDelegate(episodeDelegate{focused: !p.leftFocus})
		}
	}
}

func (p podcastModel) resizeEpisodes() {
	for _, e := range p.episodes {
		if e.list != nil {
			p.resizeRightList(e.list)
		}
	}
}

func (p podcastModel) play() (podcastModel, tea.Cmd) {
	show, ok := p.selectedShow()
	if !ok {
		return p, nil
	}
	if !p.leftFocus {
		if ep, ok := p.selectedEpisode(); ok {
			return p.playEpisode(show, ep)
		}
	}
	return p.playShow(show)
}

func (p podcastModel) playShow(show showItem) (podcastModel, tea.Cmd) {
	e := p.episodes[show.id]
	if e == nil || e.list == nil {
		*p.plays++
		p.playback = playbackStatus{seq: p.playback.seq + 1, text: "Starting " + show.title + "...", starting: true}
		p.pending = pendingPlay{seq: p.playback.seq, play: *p.plays, show: show.id}
		return p, p.loadEpisodes(show.id)
	}

	for _, ep := range listedEpisodes(e.list) {
		if ep.Playable {
			return p.playEpisode(show, ep)
		}
	}
	p.pending = pendingPlay{}
	p.playback = playbackStatus{seq: p.playback.seq + 1, text: show.title + " has no episodes to play", err: true}
	return p, nil
}

func (p podcastModel) playPending(showID libspotify.ID) (podcastModel, tea.Cmd) {
	if p.pending.show != showID {
		return p, nil
	}
	pending := p.pending
	p.pending = pendingPlay{}
	if pending.seq != p.playback.seq {
		return p, nil
	}
	if pending.play != *p.plays {
		p.playback = playbackStatus{seq: p.playback.seq}
		return p, nil
	}
	show, ok := p.findShow(showID)
	if !ok {
		return p, nil
	}
	return p.playShow(show)
}

func (p podcastModel) playEpisode(show showItem, ep spotify.Episode) (podcastModel, tea.Cmd) {
	p.pending = pendingPlay{}
	if !ep.Playable {
		p.playback = playbackStatus{seq: p.playback.seq + 1, text: ep.Name + " is unavailable", err: true}
		return p, nil
	}

	*p.plays++
	p.playback = playbackStatus{seq: p.playback.seq + 1, text: "Starting " + ep.Name + "...", starting: true}
	return p, p.startEpisode(p.playback.seq, show, ep)
}

func (p podcastModel) startEpisode(seq int, show showItem, ep spotify.Episode) tea.Cmd {
	var deviceID libspotify.ID
	if p.playerDevice != nil {
		deviceID = p.playerDevice.ID
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(p.ctx, playbackTimeout)
		defer cancel()

		if ep.ID != "" {
			readCtx, cancelRead := context.WithTimeout(ctx, episodeReadTimeout)
			latest, err := p.spotifyClient.Episode(readCtx, ep.ID)
			cancelRead()
			if err == nil && latest.Duration > 0 {
				ep.FullyPlayed, ep.Resume, ep.Duration = latest.FullyPlayed, latest.Resume, latest.Duration
			}
		}

		var from time.Duration
		if ep.InProgress() {
			from = ep.Resume
		}
		if err := p.spotifyClient.PlayEpisode(ctx, deviceID, show.uri, ep.URI, from); err != nil {
			return episodePlaybackFailed{seq, err}
		}
		return episodePlaybackStarted{seq: seq, show: show.uri, episode: ep.URI, name: ep.Name, showName: show.title, from: from, duration: ep.Duration}
	}
}

func (msg episodePlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Artist: msg.showName, Duration: msg.duration, Elapsed: msg.from, Playing: true}
}

func (p podcastModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, p.leftPane(p.list, p.msg, p.alignHorizontal), p.rightView()))
}

func (p podcastModel) rightView() string {
	if p.list == nil {
		return ""
	}

	target := "episodes"
	if !p.leftFocus {
		target = "podcasts"
	}
	keys := p.keyHelp(p.list.Help.Styles, target, p.capturesInput())
	if show, ok := p.selectedShow(); ok {
		return p.rightPane(show.title, episodesProgress(p.episodes[show.id]), p.episodesView, keys)
	}
	if len(p.list.Items()) == 0 {
		return p.rightPane("", "", func() string { return p.notice(noPodcasts) }, keys)
	}
	return ""
}

func (p podcastModel) episodesView() string {
	e := p.selectedEpisodes()
	switch {
	case e == nil:
		return p.notice("Loading episodes...")
	case e.state == stateEpisodesError:
		return p.notice(fmt.Sprintf("Fail to load episodes:\n%s\n\nPress r to try again.", errorText(e.err)))
	case e.list == nil:
		return p.notice("Loading episodes...")
	}
	return e.list.View()
}

func episodesProgress(e *showEpisodes) string {
	switch {
	case e == nil:
		return ""
	case e.refreshing:
		return " (refreshing...)"
	case e.loading && e.list != nil:
		return fmt.Sprintf(" (loading %d of %d...)", e.loaded, e.total)
	case e.err != nil && e.list != nil:
		return " (fail to load: " + errorText(e.err) + ")"
	case e.list != nil && !e.last:
		return fmt.Sprintf(" (%d of %d)", e.loaded, e.total)
	}
	return ""
}
