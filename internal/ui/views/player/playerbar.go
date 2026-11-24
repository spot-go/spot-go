package player

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	libspotify "github.com/zmb3/spotify/v2"
)

type (
	// playerBar is what the player at the bottom of the window shows next to
	// the song or episode of the active tab: the device Spotify plays on, its
	// volume, shuffle and repeat, and a notice about the last change.
	playerBar struct {
		// idle is true when the last poll found Spotify playing on no
		// device, and spot-go started nothing since.
		idle       bool
		device     libspotify.ID
		deviceName string
		volume     int
		hasVolume  bool
		shuffle    bool
		// repeat is empty while spot-go doesn't know the repeat mode, and
		// then the player hides shuffle and repeat.
		repeat spotify.Repeat
		notice barNotice
	}

	// barNotice is a short message the player shows for noticeShownFor,
	// such as a new repeat mode or why the volume couldn't change.
	barNotice struct {
		seq  int
		text string
		err  bool
	}

	noticeHidden struct {
		seq int
	}

	playerHint struct {
		key, desc string
	}
)

const (
	// fullPlayerChrome is the number of lines the full player takes below
	// the tab content: its top line, its two rows, its bottom line with the
	// key hints, and the empty last line of the window.
	fullPlayerChrome = 5
	// compactPlayerChrome is the number of lines the compact player takes:
	// the bottom line of the frame, which shows what plays, and the empty
	// last line of the window.
	compactPlayerChrome = 2
	// minFullPlayerContent is how many lines the tab content keeps with the
	// full player. Shorter windows show the compact player.
	minFullPlayerContent = 8
	playerPadding        = 1
	equalizerWidth       = 3
	quietEqualizer       = "▁▁▁"
	minWideBarWidth      = 10
	volumeBarWidth       = 10
	wideBarFilled        = "━"
	wideBarHalf          = "╸"
	wideBarEmpty         = "─"
	shuffleIcon          = "⇄"
	repeatIcon           = "↻"
	repeatOneIcon        = "↻¹"
	controlsGap          = "   "
	noticeShownFor       = 3 * time.Second
	nothingPlaying       = "Nothing is playing"
)

var (
	// equalizerFrames dance while Spotify plays, one frame per second of the
	// song or episode.
	equalizerFrames = []string{"▂▅▃", "▃▇▅", "▅▃▇", "▇▂▄", "▄▆▂", "▆▄▅"}

	frameStyle = lipgloss.NewStyle().
			Foreground(styles.LipglossColorForegroundGreenSpotify)
	titleStyle = playingStyle.Bold(true)
	// dimStyle shows what is off or not played yet: the rest of a bar, the
	// equalizer while paused, shuffle and repeat while they are off.
	dimStyle = lipgloss.NewStyle().
			Foreground(styles.LipglossColorGrey)

	playerHints = []playerHint{
		{"space/k", "play/pause"},
		{"n/p", "next/previous"},
		{"+/-", "volume"},
		{"</>", "seek"},
		{"ctrl+s", "shuffle"},
		{"ctrl+r", "repeat"},
	}
)

// hasRoomForFullPlayer reports whether a window with a tab row of rowHeight
// lines has room for the full player.
func (m Model) hasRoomForFullPlayer(rowHeight int) bool {
	return m.Height-rowHeight-fullPlayerChrome >= minFullPlayerContent
}

// activeStatus is the status of the active tab: what Spotify plays, or what
// the tab is doing, such as starting a playlist.
func (m Model) activeStatus() playbackStatus {
	if shower, ok := m.tabs[m.activeTab].content.(playbackShower); ok {
		return shower.status()
	}
	return playbackStatus{}
}

// playerView is the player at the bottom of a frame width columns wide. The
// full player shows what plays in two rows, with the key hints in its
// bottom line. The compact one only has room for the bottom line of the
// frame, so it shows what plays there.
func (m Model) playerView(width int, full bool, now time.Time) string {
	status := m.activeStatus()
	if !full {
		// The bottom line is "└─ " + status + " " + "─"… + "┘", or with
		// extras "└─ " + status + " " + "─"… + " " + extras + " ─┘".
		extras := m.playerBar.compactExtras(width - 9 - minTitleWidth)
		room := width - 6
		if extras != "" {
			room = width - 9 - lipgloss.Width(extras)
		}
		return bottomLine(width, m.statusLine(status, room, now), extras)
	}

	inner := width - 2 - 2*playerPadding
	pad := strings.Repeat(" ", playerPadding)
	row := func(content string) string {
		return frameStyle.Render("│") + pad + content + pad + frameStyle.Render("│")
	}
	return strings.Join([]string{
		frameStyle.Render("├" + strings.Repeat("─", max(0, width-2)) + "┤"),
		row(m.headlineRow(status, inner, now)),
		row(m.progressRow(status, inner, now)),
		bottomLine(width, hintLine(width-6), ""),
	}, "\n")
}

// headlineRow is the first row of the full player: the song or episode with
// its artist or podcast, or what the tab is doing, and on the right a notice
// or the device Spotify plays on when it isn't spot-go.
func (m Model) headlineRow(status playbackStatus, width int, now time.Time) string {
	note := m.playerBar.note(m.ownDevice())
	if note != "" {
		note = fit(note, width-equalizerWidth-3-minTitleWidth-len(controlsGap))
	}
	room := width
	if note != "" {
		room -= lipgloss.Width(note) + len(controlsGap)
	}

	left := m.statusLine(status, room, now)
	if status.showsTrack() {
		left = equalizer(status.track, now) + " " + status.track.headline(room-equalizerWidth-1)
	}
	return spread(left, note, width)
}

// statusLine is the status of the active tab in at most width columns. When
// the tab has nothing to show and Spotify plays nothing, it says so.
func (m Model) statusLine(status playbackStatus, width int, now time.Time) string {
	if !status.showsTrack() && status.text == "" {
		if !m.playerBar.idle {
			return ""
		}
		return dimStyle.Render(fit(nothingPlaying, width))
	}
	return status.view(width, now)
}

// progressRow is the second row of the full player: a progress bar with the
// elapsed and total time under the song or episode, and on the right the
// shuffle and repeat modes and the volume. When the row is too narrow, the
// volume drops its bar, then shuffle and repeat go, then the progress bar.
func (m Model) progressRow(status playbackStatus, width int, now time.Time) string {
	b := m.playerBar
	modes, long, short := b.modes(), b.volumeMeter(true), b.volumeMeter(false)
	controls := []string{
		joined(controlsGap, modes, long),
		joined(controlsGap, modes, short),
		short,
		"",
	}

	info := status.track.info
	if !status.showsTrack() || info.Duration <= 0 {
		for _, c := range controls {
			if lipgloss.Width(c) <= width {
				return spread("", c, width)
			}
		}
		return spread("", "", width)
	}

	elapsed := status.track.elapsed(now)
	clock := green(formatDuration(elapsed) + " / " + formatDuration(info.Duration))
	indent := strings.Repeat(" ", equalizerWidth+1)
	for _, c := range controls {
		gap := 0
		if c != "" {
			gap = len(controlsGap) + lipgloss.Width(c)
		}
		barWidth := width - len(indent) - lipgloss.Width(clock) - 1 - gap
		if barWidth >= minWideBarWidth {
			return spread(indent+wideBar(int64(elapsed), int64(info.Duration), barWidth)+" "+clock, c, width)
		}
	}
	if lipgloss.Width(indent+clock) <= width {
		return spread(indent+clock, "", width)
	}
	return spread(fit(clock, width), "", width)
}

// headline is the song or episode in bold with its artist or podcast, after
// the play/pause icon, cut to width.
func (t track) headline(width int) string {
	icon := playIcon
	if t.info.Playing {
		icon = pauseIcon
	}
	title := trackTitle{name: strings.TrimSpace(printable(t.info.Title)), artist: strings.TrimSpace(printable(t.info.Artist))}
	title = title.cut(width - lipgloss.Width(icon) - 1)
	text := titleStyle.Render(title.name)
	switch {
	case title.name == "":
		text = green(title.artist)
	case title.artist != "":
		text += green(artistSeparator + title.artist)
	}
	return fit(spaced(green(icon), text), width)
}

// equalizer dances while t plays, a frame for each second of t, and lies
// flat while t is paused.
func equalizer(t track, now time.Time) string {
	if !t.info.Playing {
		return dimStyle.Render(quietEqualizer)
	}
	frame := int(t.elapsed(now)/time.Second) % len(equalizerFrames)
	return green(equalizerFrames[frame])
}

// wideBar is a bar width columns wide, filled for value out of total in
// steps of half a column.
func wideBar(value, total int64, width int) string {
	if width <= 0 {
		return ""
	}
	halves := 0
	if total > 0 {
		halves = int(max(0, min(int64(2*width), int64(2*width)*value/total)))
	}
	filled := strings.Repeat(wideBarFilled, halves/2)
	if halves%2 == 1 {
		filled += wideBarHalf
	}
	bar := green(filled)
	if empty := width - halves/2 - halves%2; empty > 0 {
		bar += dimStyle.Render(strings.Repeat(wideBarEmpty, empty))
	}
	return bar
}

// modes shows shuffle and repeat, lit while they are on. It is empty while
// spot-go doesn't know them.
func (b playerBar) modes() string {
	if b.repeat == "" {
		return ""
	}
	repeat := repeatIcon
	if b.repeat == spotify.RepeatTrack {
		repeat = repeatOneIcon
	}
	return modeStyle(b.shuffle).Render(shuffleIcon) + "  " +
		modeStyle(b.repeat == spotify.RepeatContext || b.repeat == spotify.RepeatTrack).Render(repeat)
}

func modeStyle(on bool) lipgloss.Style {
	if on {
		return playingStyle
	}
	return dimStyle
}

// volumeMeter shows the volume of the device Spotify plays on, with a bar
// when long is true. It is empty when the device has no volume Spotify can
// change.
func (b playerBar) volumeMeter(long bool) string {
	if !b.hasVolume {
		return ""
	}
	level := green(fmt.Sprintf("%d%%", b.volume))
	if !long {
		return speaker(b.volume) + " " + level
	}
	return speaker(b.volume) + " " + wideBar(int64(b.volume), maxVolume, volumeBarWidth) + " " + level
}

func speaker(percent int) string {
	switch {
	case percent <= 0:
		return "🔇"
	case percent < 34:
		return "🔈"
	case percent < 67:
		return "🔉"
	}
	return "🔊"
}

// note is the notice while it shows, or else the device Spotify plays on
// when it isn't own.
func (b playerBar) note(own libspotify.ID) string {
	if b.notice.text != "" {
		return b.notice.render()
	}
	if b.device == "" || b.device == own || b.deviceName == "" {
		return ""
	}
	return playingStyle.Render("on " + printable(b.deviceName))
}

func (n barNotice) render() string {
	style := playingStyle
	if n.err {
		style = playbackErrorStyle
	}
	return style.Render(printable(n.text))
}

// compactExtras is what the compact player shows after the song: the notice
// while it shows, or else shuffle, repeat and the volume, in at most width
// columns.
func (b playerBar) compactExtras(width int) string {
	if width <= 0 {
		return ""
	}
	if b.notice.text != "" {
		return fit(b.notice.render(), width)
	}
	for _, extras := range []string{joined("  ", b.modes(), b.volumeMeter(false)), b.volumeMeter(false)} {
		if lipgloss.Width(extras) <= width {
			return extras
		}
	}
	return ""
}

// started shows that spot-go started playing on its own device.
func (b playerBar) started(own libspotify.ID) playerBar {
	b.idle = false
	if own != "" {
		b.device = own
	}
	return b
}

func (m Model) ownDevice() libspotify.ID {
	if m.playerDevice == nil {
		return ""
	}
	return m.playerDevice.ID
}

// hintLine shows the keys that control Spotify on every tab, in at most
// width columns. Hints that don't fit are left out from the end.
func hintLine(width int) string {
	return appendHints(styles.Help(), "", playerHints, width)
}

// appendHints adds to text as many of hints as fit in width columns, in
// order, each after a separator when text isn't empty. It stops at the
// first hint that doesn't fit.
func appendHints(h help.Styles, text string, hints []playerHint, width int) string {
	for _, hint := range hints {
		next := hintText(h, []playerHint{hint})
		if text != "" {
			next = h.ShortSeparator.Inline(true).Render(" • ") + next
		}
		if lipgloss.Width(text+next) > width {
			break
		}
		text += next
	}
	return text
}

// bottomLine is the bottom line of the frame, width columns wide, with left
// at its start and right at its end.
func bottomLine(width int, left, right string) string {
	return frameEdge("└", "┘", width, left, right)
}

// frameEdge is the top or bottom line of a frame, width columns wide, from
// the corner first to the corner last, with left at its start and right at
// its end. When they don't fit, the line is plain.
func frameEdge(first, last string, width int, left, right string) string {
	if width < 2 {
		return frameStyle.Render(strings.Repeat("─", max(0, width)))
	}
	start, end := frameStyle.Render(first), frameStyle.Render(last)
	if left != "" {
		start = frameStyle.Render(first+"─") + " " + left + " "
	}
	if right != "" {
		end = " " + right + " " + frameStyle.Render("─"+last)
	}
	fill := width - lipgloss.Width(start) - lipgloss.Width(end)
	if fill < 0 {
		return frameStyle.Render(first + strings.Repeat("─", width-2) + last)
	}
	return start + frameStyle.Render(strings.Repeat("─", fill)) + end
}

// joined joins the parts that aren't empty with gap between them.
func joined(gap string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), gap)
}

// spread puts left at the start and right at the end of a line width columns
// wide.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		return fit(left+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) notify(text string, err bool) (Model, tea.Cmd) {
	seq := m.playerBar.notice.seq + 1
	m.playerBar.notice = barNotice{seq: seq, text: text, err: err}
	return m, m.after(noticeShownFor, noticeHidden{seq: seq})
}

func (m Model) noticeHidden(msg noticeHidden) Model {
	if msg.seq == m.playerBar.notice.seq {
		m.playerBar.notice = barNotice{seq: msg.seq}
	}
	return m
}

// followPolled shows the device, volume, shuffle and repeat Spotify polled.
// A volume, shuffle or repeat changed from spot-go a moment ago keeps what
// spot-go set, as Spotify can take a moment to report it. Spotify can also
// take a moment to report a play spot-go started, so the player doesn't say
// nothing is playing right after one.
func (m Model) followPolled(playing spotify.NowPlaying) Model {
	b := &m.playerBar
	if playing.Device != b.device {
		// Shuffle and repeat changed on the device Spotify played on before
		// say nothing about this one.
		for i := range m.modes {
			m.modes[i].at = time.Time{}
		}
	}
	b.idle = playing.Device == "" && time.Since(m.nowPlaying.commandAt) >= followGrace
	b.device, b.deviceName = playing.Device, playing.DeviceName
	if !m.volume.changing() {
		b.volume, b.hasVolume = playing.Volume, playing.SupportsVolume
	}
	if !m.modes[shuffleMode].changing() {
		b.shuffle = playing.Shuffle
	}
	if !m.modes[repeatMode].changing() {
		b.repeat = playing.Repeat
	}
	return m
}
