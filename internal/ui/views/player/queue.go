package player

import (
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
		help      help.Styles
		gen       int
		loading   bool
		err       error
		playing   *spotify.PlaylistItem
		list      *list.Model
		visible   bool
		skipping  bool
		skippedAt time.Time
		playback  playbackStatus
		volume    volumeLevel
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
		position int
		item     libspotify.URI
		name     string
		artist   string
		duration time.Duration
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
		if msg.seq != q.playback.seq {
			return q, nil
		}
		q.playback = playingStatus(msg.seq, msg.item, msg.info(), time.Now())
		q.skippedAt = time.Now()
		q.skippedTo(msg)
		return q, nil

	case queuePlaybackFailed:
		q.skipping = false
		if msg.seq != q.playback.seq {
			return q, nil
		}
		q.playback = playbackStatus{seq: msg.seq, text: "Fail to play: " + errorText(msg.err), err: true}
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
		// Right after a skip, Spotify may still show the queue from before
		// it, which would bring back the songs skipped.
		if q.loading || time.Since(q.skippedAt) < followGrace {
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

func (q *queueModel) load() tea.Cmd {
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

// skippedTo shows the queue as it is after the skip to msg's item, before
// Spotify confirms it: the item plays, and the items after it come next.
func (q *queueModel) skippedTo(msg queuePlaybackStarted) {
	if q.list == nil {
		return
	}
	items := q.list.Items()
	position := msg.position
	if position >= len(items) || items[position].(songItem).URI != msg.item {
		position = -1
		for i, li := range items {
			if li.(songItem).URI == msg.item {
				position = i
				break
			}
		}
		if position < 0 {
			return
		}
	}

	playing := items[position].(songItem).PlaylistItem
	q.playing = &playing
	next := make([]spotify.PlaylistItem, 0, len(items)-position-1)
	for _, li := range items[position+1:] {
		song := li.(songItem).PlaylistItem
		song.Position = len(next)
		next = append(next, song)
	}
	q.list.SetItems(songItems(next))
	q.list.Select(0)
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
	q.skipping = true
	q.playback = playbackStatus{seq: q.playback.seq + 1, text: "Starting " + song.Name + "...", starting: true}
	return q, q.skipTo(q.playback.seq, song.PlaylistItem)
}

// skipTo plays song by skipping to it, as the Web API can't play an item of
// the queue: Spotify skips the items before it and keeps the rest of the
// queue and its context.
func (q queueModel) skipTo(seq int, song spotify.PlaylistItem) tea.Cmd {
	ctx, client := q.ctx, q.spotifyClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		for range song.Position + 1 {
			if err := client.Next(ctx); err != nil {
				return queuePlaybackFailed{seq, err}
			}
		}
		return queuePlaybackStarted{
			seq:      seq,
			position: song.Position,
			item:     song.URI,
			name:     song.Name,
			artist:   song.Artist(),
			duration: song.Duration,
		}
	}
}

func (msg queuePlaybackStarted) info() media.Info {
	return media.Info{Title: msg.name, Artist: msg.artist, Duration: msg.duration, Playing: true}
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
		if q.err != nil {
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
