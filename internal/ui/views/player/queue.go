package player

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	// queueModel is the Queue tab: what Spotify plays now on the left, and
	// what it plays next on the right.
	queueModel struct {
		playerContext
		paneSize
		help     help.Styles
		gen      int
		loading  bool
		err      error
		playing  *spotify.PlaylistItem
		list     *list.Model
		visible  bool
		skipping bool
		playback playbackStatus
		volume   volumeLevel
		// confirmEvery is how often a skip reads what Spotify plays, until it
		// plays the item skipped to.
		confirmEvery time.Duration
	}

	// tabShown and tabHidden tell a tab that it became the active tab or
	// stopped being it.
	tabShown  struct{}
	tabHidden struct{}

	queueLoaded struct {
		gen   int
		queue spotify.Queue
	}

	queueLoadFailed struct {
		gen int
		err error
	}

	queuePlaybackStarted struct {
		seq      int
		plays    int
		item     libspotify.URI
		name     string
		artist   string
		duration time.Duration
		from     time.Duration
		paused   bool
	}

	queuePlaybackFailed struct {
		seq int
		err error
	}
)

const (
	queueTimeout        = 15 * time.Second
	queueNameLines      = 3
	queueArtistLines    = 2
	queueLoadingText    = "Loading the queue..."
	queueNothingPlaying = "Spotify is not playing anything."
	queueEmptyText      = "Nothing plays next."
)

func newQueueModel(ctx playerContext) queueModel {
	return queueModel{
		playerContext: ctx,
		help:          help.DefaultStyles(styles.HasDarkBackground),
		confirmEvery:  confirmPollEvery,
	}
}

func (q queueModel) Init() tea.Cmd {
	return nil
}

func (q queueModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return q.handleKey(msg)

	case contentDimension:
		q.paneSize = paneSize(msg)
		if q.list != nil {
			q.resizeRightList(q.list)
		}
		return q, nil

	case tabShown:
		q.visible = true
		if q.loading {
			return q, nil
		}
		cmd := q.load()
		return q, cmd

	case tabHidden:
		q.visible = false
		return q, nil

	case queueLoaded:
		if msg.gen == q.gen {
			q.loaded(msg.queue)
		}
		return q, nil

	case queueLoadFailed:
		if msg.gen == q.gen {
			q.loading, q.err = false, msg.err
		}
		return q, nil

	case queuePlaybackStarted:
		q.skipping = false
		if msg.seq == q.playback.seq {
			q.playback = playingStatus(msg.seq, msg.item, msg.info(), time.Now())
		}
		cmd := q.reload()
		return q, cmd

	case queuePlaybackFailed:
		q.skipping = false
		if msg.seq == q.playback.seq {
			q.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
		}
		// Some of the skips may have gone through.
		cmd := q.reload()
		return q, cmd

	case playbackToggled:
		if msg.plays == *q.plays {
			q.playback = q.playback.toggled(msg, time.Now())
		}
		if !msg.skip || msg.err != nil {
			return q, nil
		}
		cmd := q.reload()
		return q, cmd

	case volumeChanged:
		q.volume = q.volume.changed(msg)
		return q, nil

	case volumeHidden:
		q.volume = q.volume.hidden(msg)
		return q, nil

	case playbackConfirmed:
		q.playback = q.playback.followNow(msg.playing, time.Now())
		cmd := q.reload()
		return q, cmd

	case playbackPolled:
		if msg.err != nil {
			return q, nil
		}
		if msg.plays == *q.plays {
			q.playback = q.playback.follow(msg.playing, time.Now())
		}
		if q.loading {
			return q, nil
		}
		cmd := q.reload()
		return q, cmd
	}

	return q, nil
}

func (q queueModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		cmd := q.load()
		return q, cmd

	case "enter":
		return q.play()
	}

	if q.list == nil {
		return q, nil
	}
	l, cmd := q.list.Update(msg)
	q.list = &l
	return q, cmd
}

func (q queueModel) status() playbackStatus {
	return q.playback
}

// reload loads the queue again while the tab shows it. A hidden tab loads it
// when it shows again.
func (q *queueModel) reload() tea.Cmd {
	if !q.visible {
		return nil
	}
	return q.load()
}

// load loads the queue again. A skip loads it once it ends, as a queue read
// while it skips would show items it skips.
func (q *queueModel) load() tea.Cmd {
	if q.skipping {
		return nil
	}
	q.gen++
	q.loading = true
	gen, ctx, client := q.gen, q.ctx, q.spotifyClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, queueTimeout)
		defer cancel()

		queue, err := client.Queue(ctx)
		if err != nil {
			return queueLoadFailed{gen, err}
		}
		return queueLoaded{gen, queue}
	}
}

func (q *queueModel) loaded(queue spotify.Queue) {
	q.loading, q.err = false, nil
	q.playing = queue.Playing
	if q.list == nil {
		q.list = q.newRightList(songItems(queue.Items), songDelegate{focused: true}, "item", "items")
		return
	}
	index := reselect(q.list, queue.Items)
	q.list.SetItems(songItems(queue.Items))
	q.list.Select(index)
}

func (q queueModel) play() (tea.Model, tea.Cmd) {
	if q.skipping || q.list == nil {
		return q, nil
	}
	song, ok := q.list.SelectedItem().(songItem)
	if !ok {
		return q, nil
	}

	*q.plays++
	// A load still running would show the queue from before the skip.
	q.gen++
	q.loading, q.skipping = false, true
	q.playback = playbackStatus{seq: q.playback.seq + 1, text: "Starting " + song.Name + "...", starting: true}
	return q, q.skipTo(q.playback.seq, *q.plays, song.PlaylistItem)
}

// skipTo plays song by skipping to it, as the Web API can't play an item of
// the queue: Spotify skips the items before it and keeps the rest of the
// queue and its context. The queue on screen can be seconds old, so skipTo
// reads it again to count the items before song. Then it waits for Spotify
// to play song, to show whether it plays or stays paused, and from where.
func (q queueModel) skipTo(seq, plays int, song spotify.PlaylistItem) tea.Cmd {
	ctx, client, every := q.ctx, q.spotifyClient, q.confirmEvery
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		failed := queuePlaybackFailed{seq: seq}
		queue, err := client.Queue(ctx)
		if err != nil {
			failed.err = err
			return failed
		}
		position, ok := queuePosition(queue.Items, song)
		if !ok {
			failed.err = fmt.Errorf("%s is no longer in the queue", song.Name)
			return failed
		}
		for range position + 1 {
			if failed.err = client.Next(ctx); failed.err != nil {
				return failed
			}
		}

		started := queuePlaybackStarted{seq: seq, plays: plays, item: song.URI, name: song.Name, artist: song.Artist(), duration: song.Duration}
		again := queue.Playing != nil && queue.Playing.URI == song.URI
		for range confirmPolls {
			if wait(ctx, every) != nil {
				break
			}
			state, err := client.Playback(ctx)
			if err != nil {
				break
			}
			if state.Item == song.URI && (!again || state.Progress < followGrace) {
				started.from, started.paused = state.Progress, !state.Playing
				started.duration = cmp.Or(state.Duration, started.duration)
				break
			}
		}
		return started
	}
}

// queuePosition finds song in items: the position of the item with its URI
// nearest to where song was.
func queuePosition(items []spotify.PlaylistItem, song spotify.PlaylistItem) (int, bool) {
	distance := func(position int) int {
		if position < song.Position {
			return song.Position - position
		}
		return position - song.Position
	}
	best := -1
	for _, item := range items {
		if item.URI == song.URI && (best < 0 || distance(item.Position) < distance(best)) {
			best = item.Position
		}
	}
	return best, best >= 0
}

func (msg queuePlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Artist: msg.artist, Duration: msg.duration, Elapsed: msg.from, Playing: !msg.paused}
}

func (q queueModel) View() tea.View {
	return tea.NewView(lipgloss.JoinHorizontal(lipgloss.Top, q.leftView(), q.rightView()))
}

func (q queueModel) leftView() string {
	width := q.leftWidth()
	lines := []string{paneTitleStyle.Render("Queue"), ""}
	if q.playing != nil {
		room := max(0, width-1)
		lines = append(lines, wrapLines(q.help.ShortDesc.Inline(true), "Now playing", room, 2)...)
		lines = append(lines, wrapLines(playingStyle, q.playing.Name, room, queueNameLines)...)
		lines = append(lines, wrapLines(songStyle, q.playing.Artist(), room, queueArtistLines)...)
	}

	body := lipgloss.NewStyle().
		PaddingLeft(1).
		Height(q.height).
		MaxHeight(q.height).
		Render(strings.Join(lines, "\n"))
	return borderStyle.
		Height(q.height).
		MaxHeight(q.height).
		Width(width + borderStyle.GetHorizontalBorderSize()).
		Render(body)
}

// wrapLines wraps s to width, in at most limit lines. The last line ends in
// … when s doesn't fit.
func wrapLines(style lipgloss.Style, s string, width, limit int) []string {
	s = strings.TrimSpace(printable(s))
	if s == "" || width <= 0 || limit <= 0 {
		return nil
	}
	lines := strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n")
	if len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = clip(strings.TrimRight(lines[limit-1], " ")+"…", width)
	}
	for i, line := range lines {
		lines[i] = style.Render(fit(strings.TrimRight(line, " "), width))
	}
	return lines
}

func (q queueModel) rightView() string {
	keys := q.keyHelp(q.help, "")
	if q.list == nil {
		text := queueLoadingText
		if q.err != nil && !q.loading {
			text = fmt.Sprintf("Fail to load the queue:\n%s\n\nPress r to try again.", errorText(q.err))
		}
		return q.rightPane("", "", func() string { return q.notice(text) }, q.playback, q.volume, keys)
	}

	note := ""
	if q.err != nil {
		note = " (fail to load: " + errorText(q.err) + ")"
	}
	return q.rightPane("Next up", note, func() string {
		switch {
		case len(q.list.Items()) > 0:
			return q.list.View()
		case q.playing == nil:
			return q.notice(queueNothingPlaying)
		}
		return q.notice(queueEmptyText)
	}, q.playback, q.volume, keys)
}
