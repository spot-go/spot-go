package player

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/ui/styles"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	// likeControl is whether the song or episode the player shows is in
	// your library, as the + and ✓ of the Spotify app show it: in Liked
	// Songs for a song, in Your Episodes for an episode.
	likeControl struct {
		// item is the song or episode the player shows, which the rest is
		// about. It is empty when the player shows nothing spot-go can like.
		item libspotify.URI
		// seq tells what Spotify answers about item apart from older
		// answers: it changes with item, and when a like or unlike starts.
		seq int
		// known is true while spot-go knows whether item is liked, and liked
		// says it. A like or unlike shows at once, before Spotify answers.
		known bool
		liked bool
		// checking is true while spot-go asks Spotify whether item is liked,
		// and busy while it likes or unlikes it.
		checking bool
		busy     bool
		// checkedAt is when Spotify last answered about item.
		checkedAt time.Time
	}

	likeChecked struct {
		item  libspotify.URI
		seq   int
		liked bool
		err   error
	}

	// likeChanged is the end of a like, when liked is true, or of an
	// unlike. known is false when spot-go didn't know whether item was
	// liked, and failed to read it before the change.
	likeChanged struct {
		item  libspotify.URI
		seq   int
		liked bool
		known bool
		err   error
	}
)

const (
	likedMark   = "✓"
	unlikedMark = "+"
	// likeRecheckEvery is how often spot-go asks Spotify again whether the
	// song or episode that plays is liked, as it may have been liked or
	// unliked on another device meanwhile.
	likeRecheckEvery = 30 * time.Second

	songURIPrefix    = "spotify:track:"
	episodeURIPrefix = "spotify:episode:"
	localURIPrefix   = "spotify:local:"
)

var (
	likedStyle = playingStyle.Bold(true)

	errLocalFile = errors.New("local files can't be liked")
)

// likeable is item when spot-go can like it: a song or an episode on Spotify.
func likeable(item libspotify.URI) libspotify.URI {
	s := string(item)
	if strings.HasPrefix(s, songURIPrefix) && len(s) > len(songURIPrefix) ||
		strings.HasPrefix(s, episodeURIPrefix) && len(s) > len(episodeURIPrefix) {
		return item
	}
	return ""
}

// collection is where a like puts item: Liked Songs, or Your Episodes for
// an episode.
func collection(item libspotify.URI) string {
	if strings.HasPrefix(string(item), episodeURIPrefix) {
		return "Your Episodes"
	}
	return likedSongs
}

// shownItem is the song or episode the player shows, or "" when it shows
// none.
func (m Model) shownItem() libspotify.URI {
	status := m.activeStatus()
	if !status.showsTrack() {
		return ""
	}
	return status.track.item
}

// followLike keeps the like state on the song or episode the player shows:
// a new one is checked at once, and the one that plays again every
// likeRecheckEvery.
func (m Model) followLike(cmd tea.Cmd) (Model, tea.Cmd) {
	item := likeable(m.shownItem())
	if item != m.like.item {
		m.like = likeControl{item: item, seq: m.like.seq + 1}
		if item == "" {
			return m, cmd
		}
		return m.checkLike(cmd)
	}
	if item == "" || m.like.busy || m.like.checking ||
		!m.activeStatus().track.info.Playing || time.Since(m.like.checkedAt) < likeRecheckEvery {
		return m, cmd
	}
	return m.checkLike(cmd)
}

// checkLike asks Spotify whether the song or episode the player shows is
// liked.
func (m Model) checkLike(cmd tea.Cmd) (Model, tea.Cmd) {
	m.like.checking = true
	item, seq := m.like.item, m.like.seq
	ctx, client := m.ctx, m.spotifyClient
	return m, tea.Batch(cmd, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		liked, err := client.InLibrary(ctx, item)
		return likeChecked{item: item, seq: seq, liked: liked, err: err}
	})
}

// likeChecked shows what Spotify answered about the song or episode the
// player shows. A failure keeps what spot-go knew, and the next check, or
// l, tries again.
func (m Model) likeChecked(msg likeChecked) Model {
	if msg.item != m.like.item || msg.seq != m.like.seq {
		return m
	}
	m.like.checking, m.like.checkedAt = false, time.Now()
	if msg.err == nil {
		m.like.known, m.like.liked = true, msg.liked
	}
	return m
}

// toggleLike likes the song or episode the player shows, or unlikes it when
// it is liked. The player shows the change at once. Presses while a change
// runs flip what the player shows, and once the change ends, spot-go
// changes it again if it differs. When spot-go doesn't know whether it is
// liked, it reads it first.
func (m Model) toggleLike() (Model, tea.Cmd) {
	if m.like.item == "" {
		shown := m.shownItem()
		err := errNothingPlaying
		if strings.HasPrefix(string(shown), localURIPrefix) {
			err = errLocalFile
		}
		return m.notify("Fail to like: "+err.Error(), true)
	}
	if m.like.busy {
		if m.like.known {
			m.like.liked = !m.like.liked
		}
		return m, nil
	}

	if !m.like.known {
		return m.changeLike(nil)
	}
	liked := !m.like.liked
	return m.changeLike(&liked)
}

// changeLike likes or unlikes the song or episode the player shows, as
// liked says, or the other way around from what Spotify says when liked is
// nil.
func (m Model) changeLike(liked *bool) (Model, tea.Cmd) {
	m.like.seq++
	m.like.busy, m.like.checking = true, false
	if liked != nil {
		m.like.liked = *liked
	}

	changed := likeChanged{item: m.like.item, seq: m.like.seq, known: liked != nil}
	ctx, client := m.ctx, m.spotifyClient
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, playbackTimeout)
		defer cancel()

		if liked != nil {
			changed.liked = *liked
		} else {
			saved, err := client.InLibrary(ctx, changed.item)
			if err != nil {
				changed.err = err
				return changed
			}
			changed.liked, changed.known = !saved, true
		}

		if changed.liked {
			changed.err = client.AddToLibrary(ctx, changed.item)
		} else {
			changed.err = client.RemoveFromLibrary(ctx, changed.item)
		}
		return changed
	}
}

// likeChanged shows how a like or unlike ended, and tells the tabs when it
// changed Liked Songs. A failure shows what was liked before.
func (m Model) likeChanged(msg likeChanged) (Model, tea.Cmd) {
	current := msg.item == m.like.item && msg.seq == m.like.seq
	if msg.err != nil {
		if current {
			m.like.busy, m.like.checkedAt = false, time.Now()
			m.like.known, m.like.liked = msg.known, !msg.liked
		}
		return m.notify(msg.failure()+errorText(msg.err), true)
	}

	var again tea.Cmd
	if current {
		m.like.busy, m.like.checkedAt = false, time.Now()
		if m.like.known && m.like.liked != msg.liked {
			// l was pressed while the change ran.
			liked := m.like.liked
			m, again = m.changeLike(&liked)
		} else {
			m.like.known, m.like.liked = true, msg.liked
		}
	}
	m, notice := m.notify(msg.text(), false)
	return m, tea.Batch(notice, again, m.broadcast(msg))
}

func (msg likeChanged) text() string {
	if msg.liked {
		return "Added to " + collection(msg.item)
	}
	return "Removed from " + collection(msg.item)
}

func (msg likeChanged) failure() string {
	switch {
	case !msg.known:
		return "Fail to like: "
	case msg.liked:
		return "Fail to add to " + collection(msg.item) + ": "
	}
	return "Fail to remove from " + collection(msg.item) + ": "
}

// mark is ✓ when item, the song or episode the player shows, is liked, and
// + when it isn't. It is empty while spot-go doesn't know.
func (l likeControl) mark(item libspotify.URI) string {
	switch {
	case item == "" || item != l.item || !l.known:
		return ""
	case l.liked:
		return likedStyle.Render(likedMark)
	}
	// + shows in the gray of what the keys do, as it says what l does: it
	// reads on the background of the terminal, unlike the dim gray of what
	// is off.
	return lipgloss.NewStyle().Foreground(styles.Help().ShortDesc.GetForeground()).Render(unlikedMark)
}
