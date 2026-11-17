package player

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	track struct {
		item libspotify.URI
		info media.Info
		at   time.Time
	}

	trackTitle struct {
		name   string
		artist string
	}

	playbackStatus struct {
		seq      int
		text     string
		err      bool
		starting bool
		track    track
		since    time.Time
	}

	playbackShower interface {
		status() playbackStatus
	}

	progressTicked struct{}
)

const (
	pauseIcon        = "⏸"
	playIcon         = "▶"
	trackDecoration  = "🎧"
	artistSeparator  = " — "
	progressBarWidth = 10
	progressFilled   = "█"
	progressEmpty    = "░"
	minTitleWidth    = 10
	minArtistWidth   = 4
	progressInterval = time.Second
)

var progressEmptyStyle = lipgloss.NewStyle().
	Foreground(styles.LipglossColorDarkGreenShade).
	Background(styles.LipglossColorDarkGreen)

func (t track) elapsed(now time.Time) time.Duration {
	elapsed := t.info.Elapsed
	if t.info.Playing {
		elapsed += now.Sub(t.at)
	}
	if t.info.Duration > 0 {
		elapsed = min(elapsed, t.info.Duration)
	}
	return elapsed
}

func (t track) paused(playing spotify.NowPlaying, now time.Time) (track, bool) {
	same := playing.PausedItem != "" && playing.PausedItem == t.item
	switch {
	case same && (t.info.Playing || t.info.Elapsed != playing.PausedProgress):
		t.info.Elapsed = playing.PausedProgress
	case t.info.Playing:
		t.info.Elapsed = t.elapsed(now)
	default:
		return t, false
	}
	t.info.Playing, t.at = false, now
	return t, true
}

func (t track) toggled(msg playbackToggled, now time.Time) track {
	if msg.item == "" && msg.name == "" && !t.at.IsZero() {
		t.info.Elapsed, t.info.Playing, t.at = t.elapsed(now), !msg.paused, now
		return t
	}
	return track{item: msg.item, info: msg.info(), at: now}
}

func (t track) line(width int, now time.Time) string {
	icon := playIcon
	if t.info.Playing {
		icon = pauseIcon
	}
	title := trackTitle{name: strings.TrimSpace(printable(t.info.Title)), artist: strings.TrimSpace(printable(t.info.Artist))}
	var bar, clock string
	if duration := t.info.Duration; duration > 0 {
		elapsed := t.elapsed(now)
		bar = progressBar(elapsed, duration)
		clock = green(formatDuration(elapsed) + " / " + formatDuration(duration))
	}

	layouts := [][2][]string{
		{{trackDecoration, green(icon)}, {bar, clock, trackDecoration}},
		{{green(icon)}, {bar, clock}},
		{{green(icon)}, {clock}},
	}
	for _, l := range layouts {
		if line, ok := fitTitle(width, l[0], title, l[1]); ok {
			return line
		}
	}
	return green(fit(spaced(icon, title.fit(width-lipgloss.Width(icon)-1)), width))
}

func (t trackTitle) String() string {
	if t.name == "" || t.artist == "" {
		return t.name + t.artist
	}
	return t.name + artistSeparator + t.artist
}

func (t trackTitle) fit(room int) string {
	return t.cut(room).String()
}

// cut cuts the title to room columns: the artist first, down to
// minArtistWidth, and then the name without the artist.
func (t trackTitle) cut(room int) trackTitle {
	if lipgloss.Width(t.String()) <= room {
		return t
	}
	if t.name != "" && t.artist != "" {
		if artist := clip(t.artist, room-lipgloss.Width(t.name+artistSeparator)); lipgloss.Width(artist) >= minArtistWidth {
			return trackTitle{name: t.name, artist: artist}
		}
	}
	if t.name == "" {
		return trackTitle{artist: clip(t.artist, room)}
	}
	return trackTitle{name: clip(t.name, room)}
}

func clip(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	kept := strings.TrimRightFunc(ansi.Truncate(s, max(0, width-1), ""), isSeparator)
	if kept == "" {
		return ""
	}
	return kept + "…"
}

func isSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Pd, r)
}

func fitTitle(width int, before []string, title trackTitle, after []string) (string, bool) {
	join := func(title string) string {
		return spaced(slices.Concat(before, []string{green(title)}, after)...)
	}
	full := title.String()
	over := lipgloss.Width(join(full)) - width
	if over <= 0 {
		return join(full), true
	}
	room := lipgloss.Width(full) - over
	if room < minTitleWidth {
		return "", false
	}
	return join(title.fit(room)), true
}

func spaced(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " ")
}

func green(s string) string {
	if s == "" {
		return ""
	}
	return playingStyle.Render(s)
}

func progressBar(elapsed, duration time.Duration) string {
	filled := max(0, min(progressBarWidth, int(int64(progressBarWidth)*int64(elapsed)/int64(duration))))
	bar := green(strings.Repeat(progressFilled, filled))
	if empty := progressBarWidth - filled; empty > 0 {
		bar += progressEmptyStyle.Render(strings.Repeat(progressEmpty, empty))
	}
	return bar
}

func playingStatus(seq int, item libspotify.URI, info media.Info, now time.Time) playbackStatus {
	return playbackStatus{seq: seq, track: track{item: item, info: info, at: now}, since: now}
}

func (s playbackStatus) showsTrack() bool {
	return !s.track.at.IsZero()
}

func (s playbackStatus) view(width int, now time.Time) string {
	if s.showsTrack() {
		return s.track.line(width, now)
	}
	style := playingStyle
	if s.err {
		style = playbackErrorStyle
	}
	return style.Render(fit(printable(s.text), width))
}

func (s playbackStatus) nextTick(now time.Time, every time.Duration) (time.Duration, bool) {
	info := s.track.info
	if !s.showsTrack() || !info.Playing || info.Duration <= 0 {
		return 0, false
	}
	elapsed := s.track.elapsed(now)
	if elapsed >= info.Duration {
		return 0, false
	}
	return every - elapsed%every, true
}

func (s playbackStatus) follow(playing spotify.NowPlaying, now time.Time) playbackStatus {
	if now.Sub(s.since) < followGrace {
		return s
	}
	return s.followNow(playing, now)
}

func (s playbackStatus) followNow(playing spotify.NowPlaying, now time.Time) playbackStatus {
	switch {
	case s.err || s.starting || playing.Item == "" && playing.Playing:
		return s
	case playing.Item == "":
		if paused, ok := s.track.paused(playing, now); ok {
			s.track = paused
		}
		return s
	case playing.Name == "":
		return s
	}

	followed := playingStatus(s.seq, playing.Item, playingInfo(playing), now)
	followed.since = s.since
	if !s.showsTrack() || s.track.item != playing.Item {
		return followed
	}
	info := &followed.track.info
	info.Title, info.Artist = cmp.Or(s.track.info.Title, info.Title), cmp.Or(s.track.info.Artist, info.Artist)
	info.Duration = cmp.Or(info.Duration, s.track.info.Duration)
	if s.track.info.Playing && (s.track.elapsed(now)-info.Elapsed).Abs() < startSlack {
		info.Elapsed, followed.track.at = s.track.info.Elapsed, s.track.at
	}
	return followed
}

func (s playbackStatus) toggled(msg playbackToggled, now time.Time) playbackStatus {
	switch {
	case msg.err != nil && msg.skip:
		return playbackStatus{seq: s.seq, text: "Fail to skip: " + errorText(msg.err), err: true}
	case msg.err != nil && msg.seek:
		return playbackStatus{seq: s.seq, text: "Fail to seek: " + errorText(msg.err), err: true}
	case msg.err != nil:
		return playbackStatus{seq: s.seq, text: "Fail to play/pause: " + errorText(msg.err), err: true}
	}
	return playbackStatus{seq: s.seq, track: s.track.toggled(msg, now), since: now}
}
