package background

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

const soloistDevice = libspotify.ID("soloist-device")

var (
	soWhat = spotify.Playback{
		Playing:  true,
		Device:   soloistDevice,
		Item:     "spotify:track:so-what",
		Name:     "So What",
		Artist:   "Miles Davis",
		Progress: time.Minute,
		Duration: 9 * time.Minute,
	}
	freddie = spotify.Playback{
		Playing:  true,
		Device:   soloistDevice,
		Item:     "spotify:track:freddie-freeloader",
		Name:     "Freddie Freeloader",
		Artist:   "Miles Davis",
		Duration: 10 * time.Minute,
	}
)

// fakeSpotify is Spotify for a session. Pause and resume change what it
// reports only with follows, as Spotify takes a moment to report them.
type fakeSpotify struct {
	mu      sync.Mutex
	state   spotify.Playback
	follows bool
	// skipTo is what plays after a skip, once polls more polls asked.
	skipTo     spotify.Playback
	polls      int
	skipped    bool
	err        error
	commandErr error
	calls      []string
}

func (f *fakeSpotify) Playback(context.Context) (spotify.Playback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "playback")
	if f.err != nil {
		return spotify.Playback{}, f.err
	}
	if f.skipped {
		if f.polls > 0 {
			f.polls--
		} else {
			f.state, f.skipped = f.skipTo, false
		}
	}
	return f.state, nil
}

func (f *fakeSpotify) Pause(context.Context) error {
	return f.command("pause", func() { f.state.Playing = false })
}

func (f *fakeSpotify) ResumePlayback(_ context.Context, device libspotify.ID) error {
	return f.command("resume "+string(device), func() {
		f.state.Playing = true
		if device != "" {
			f.state.Device = device
		}
	})
}

func (f *fakeSpotify) Next(context.Context) error {
	return f.command("next", func() { f.skipped = true })
}

func (f *fakeSpotify) Previous(context.Context) error {
	return f.command("previous", func() { f.skipped = true })
}

func (f *fakeSpotify) command(name string, follow func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, name)
	if f.commandErr != nil {
		return f.commandErr
	}
	if f.follows || name == "next" || name == "previous" {
		follow()
	}
	return nil
}

func (f *fakeSpotify) takeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	calls := f.calls
	f.calls = nil
	return calls
}

type harness struct {
	t         *testing.T
	spotify   *fakeSpotify
	session   *session
	clock     time.Time
	mu        sync.Mutex
	published []media.Info
}

func newHarness(t *testing.T, state spotify.Playback) *harness {
	h := &harness{
		t:       t,
		spotify: &fakeSpotify{state: state, follows: true},
		clock:   time.Date(2026, 11, 17, 20, 0, 0, 0, time.UTC),
	}
	h.session = newSession(h.spotify, soloistDevice, h.publish, func() bool { return true })
	h.session.now = func() time.Time { return h.clock }
	return h
}

func (h *harness) publish(info media.Info) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.published = append(h.published, info)
}

func (h *harness) advance(d time.Duration) {
	h.clock = h.clock.Add(d)
}

func (h *harness) poll() time.Duration {
	return h.session.poll(context.Background())
}

func (h *harness) press(c media.Command) time.Duration {
	h.t.Helper()

	next, ok := h.session.command(context.Background(), c)
	if !ok {
		h.t.Fatalf("command(%d) kept the time of the next poll", c)
	}
	return next
}

// wantShown checks what Now Playing got since the last check.
func (h *harness) wantShown(want ...media.Info) {
	h.t.Helper()

	h.mu.Lock()
	got := h.published
	h.published = nil
	h.mu.Unlock()
	if len(got) != len(want) {
		h.t.Fatalf("published %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			h.t.Fatalf("published %+v, want %+v", got[i], want[i])
		}
	}
}

func (h *harness) wantCalls(want ...string) {
	h.t.Helper()

	got := h.spotify.takeCalls()
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		h.t.Fatalf("Spotify got %q, want %q", got, want)
	}
}

func shown(state spotify.Playback, elapsed time.Duration, playing bool) media.Info {
	return media.Info{
		ID:       string(state.Item),
		Title:    state.Name,
		Artist:   state.Artist,
		Duration: state.Duration,
		Elapsed:  elapsed,
		Playing:  playing,
	}
}

func TestPollShowsWhatPlays(t *testing.T) {
	h := newHarness(t, soWhat)

	if next := h.poll(); next != playingEvery {
		t.Fatalf("next poll in %v, want %v", next, playingEvery)
	}
	h.wantCalls("playback")
	h.wantShown(shown(soWhat, time.Minute, true))
}

func TestPollWhenTheSongEnds(t *testing.T) {
	ending := soWhat
	ending.Progress = ending.Duration - 2*time.Second
	h := newHarness(t, ending)

	if next := h.poll(); next != 3*time.Second {
		t.Fatalf("next poll in %v, want 3s: a second after the song ends", next)
	}
}

func TestPollWhilePaused(t *testing.T) {
	paused := soWhat
	paused.Playing = false
	h := newHarness(t, paused)

	if next := h.poll(); next != pausedEvery {
		t.Fatalf("next poll in %v, want %v", next, pausedEvery)
	}
	h.wantShown(shown(soWhat, time.Minute, false))
}

func TestPollKeepsTheLastSongPausedWhenNothingPlays(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	// Spotify has no active device a while after a pause.
	h.spotify.state = spotify.Playback{}
	h.advance(10 * time.Second)
	if next := h.poll(); next != pausedEvery {
		t.Fatalf("next poll in %v, want %v", next, pausedEvery)
	}
	h.wantShown(shown(soWhat, time.Minute+10*time.Second, false))
}

func TestPollShowsNothingBeforeItKnowsASong(t *testing.T) {
	h := newHarness(t, spotify.Playback{})

	h.poll()
	h.wantShown()
}

func TestPollKeepsWhatItShowsWhileSpotifyPlaysSomethingUnnamed(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	h.spotify.state = spotify.Playback{Playing: true, Device: soloistDevice}
	h.poll()
	h.wantShown()
}

func TestPlayPauseKeys(t *testing.T) {
	paused := soWhat
	paused.Playing = false
	elsewhere := paused
	elsewhere.Device = "phone"
	noDevice := paused
	noDevice.Device = ""

	tests := []struct {
		name    string
		state   spotify.Playback
		key     media.Command
		calls   []string
		playing bool
	}{
		{name: "toggle while playing", state: soWhat, key: media.TogglePlayPause, calls: []string{"pause"}},
		{name: "pause while playing", state: soWhat, key: media.Pause, calls: []string{"pause"}},
		{name: "play while playing", state: soWhat, key: media.Play, playing: true},
		{name: "toggle while paused", state: paused, key: media.TogglePlayPause, calls: []string{"resume "}, playing: true},
		{name: "play while paused", state: paused, key: media.Play, calls: []string{"resume "}, playing: true},
		{name: "pause while paused", state: paused, key: media.Pause},
		{name: "toggle while paused on another device", state: elsewhere, key: media.TogglePlayPause, calls: []string{"resume "}, playing: true},
		{name: "toggle with no active device", state: noDevice, key: media.TogglePlayPause, calls: []string{"resume " + string(soloistDevice)}, playing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.state)

			if next := h.press(tt.key); next != followGrace {
				t.Fatalf("next poll in %v, want %v", next, followGrace)
			}
			h.wantCalls(append([]string{"playback"}, tt.calls...)...)
			h.wantShown(shown(soWhat, time.Minute, tt.playing))
		})
	}
}

func TestPlayWithNoActiveDeviceResumesTheLastSong(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))
	h.spotify.state = spotify.Playback{}
	h.advance(time.Minute)
	h.poll()
	h.wantShown(shown(soWhat, 2*time.Minute, false))
	h.spotify.takeCalls()

	h.advance(time.Hour)
	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "resume "+string(soloistDevice))
	h.wantShown(shown(soWhat, 2*time.Minute, true))

	// A press right after it pauses on Soloist.
	h.advance(time.Second)
	h.press(media.TogglePlayPause)
	h.wantCalls("pause")
	h.wantShown(shown(soWhat, 2*time.Minute+time.Second, false))
}

func TestPlayPauseRightAfterAnotherGoesBack(t *testing.T) {
	h := newHarness(t, soWhat)
	// Spotify still reports what played before each press.
	h.spotify.follows = false

	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "pause")
	h.wantShown(shown(soWhat, time.Minute, false))

	h.advance(time.Second)
	h.press(media.TogglePlayPause)
	h.wantCalls("resume ")
	h.wantShown(shown(soWhat, time.Minute, true))

	h.advance(followGrace)
	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "pause")
}

func TestPollRightAfterAPlayPauseWaitsForSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.follows = false
	h.press(media.TogglePlayPause)
	h.spotify.takeCalls()
	h.wantShown(shown(soWhat, time.Minute, false))

	h.advance(time.Second)
	if next := h.poll(); next != followGrace-time.Second {
		t.Fatalf("next poll in %v, want %v", next, followGrace-time.Second)
	}
	h.wantCalls()
	h.wantShown()

	h.spotify.follows = true
	h.spotify.state.Playing = false
	h.advance(followGrace)
	h.poll()
	h.wantCalls("playback")
	h.wantShown(shown(soWhat, time.Minute, false))
}

func TestSkipWaitsForSpotifyToPlayTheNextSong(t *testing.T) {
	for _, key := range []media.Command{media.NextTrack, media.PreviousTrack} {
		h := newHarness(t, soWhat)
		h.poll()
		h.wantShown(shown(soWhat, time.Minute, true))
		h.spotify.takeCalls()

		h.spotify.skipTo, h.spotify.polls = freddie, 2
		if next := h.press(key); next != skipEvery {
			t.Fatalf("next poll in %v, want %v", next, skipEvery)
		}
		for range 2 {
			if next := h.poll(); next != skipEvery {
				t.Fatalf("next poll in %v while Spotify plays the song before, want %v", next, skipEvery)
			}
		}
		h.wantShown()
		if next := h.poll(); next != playingEvery {
			t.Fatalf("next poll in %v once the next song plays, want %v", next, playingEvery)
		}
		h.wantShown(shown(freddie, 0, true))
		h.wantCalls(map[media.Command]string{media.NextTrack: "next", media.PreviousTrack: "previous"}[key], "playback", "playback", "playback")
	}
}

func TestSkipToTheStartOfTheSameSong(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	restarted := soWhat
	restarted.Progress = time.Second
	h.spotify.skipTo = restarted
	h.advance(time.Second)
	h.press(media.PreviousTrack)
	if next := h.poll(); next != playingEvery {
		t.Fatalf("next poll in %v, want %v", next, playingEvery)
	}
	h.wantShown(shown(soWhat, time.Second, true))
}

func TestSkipStopsWaitingForSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	h.spotify.skipTo, h.spotify.polls = freddie, 100
	h.press(media.NextTrack)
	for range skipPolls - 1 {
		h.poll()
	}
	h.wantShown()
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))
}

func TestPlayPauseAfterASkipAsksSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.press(media.TogglePlayPause)
	h.spotify.takeCalls()

	// The pause it knows of doesn't hold after a skip.
	h.spotify.skipTo = freddie
	h.press(media.NextTrack)
	h.press(media.TogglePlayPause)
	h.wantCalls("next", "playback", "pause")
}

func TestFailedKeysPollSoon(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.commandErr = errors.New("Restriction violated")
	for _, key := range []media.Command{media.TogglePlayPause, media.NextTrack} {
		if next := h.press(key); next != skipEvery {
			t.Fatalf("next poll in %v after %d failed, want %v", next, key, skipEvery)
		}
	}
	h.wantShown()

	h.spotify.err = errors.New("offline")
	if next := h.press(media.TogglePlayPause); next != skipEvery {
		t.Fatalf("next poll in %v after Spotify didn't say what plays, want %v", next, skipEvery)
	}
	if next := h.poll(); next != pausedEvery {
		t.Fatalf("next poll in %v after a failed poll, want %v", next, pausedEvery)
	}
	h.wantShown()
}

func TestErrorsThatRepeatAreLoggedOnce(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	h := newHarness(t, soWhat)
	h.spotify.err = errors.New("offline")
	h.poll()
	h.poll()
	h.spotify.err = nil
	h.poll()
	h.spotify.err = errors.New("offline")
	h.poll()

	if n := strings.Count(out.String(), "offline"); n != 2 {
		t.Fatalf("logged %d errors, want 2:\n%s", n, out.String())
	}
}

func TestRunRunsTheKeysUntilSoloistStops(t *testing.T) {
	h := newHarness(t, soWhat)
	var stopped sync.Mutex
	running := true
	h.session.running = func() bool {
		stopped.Lock()
		defer stopped.Unlock()
		return running
	}
	h.session.now = time.Now
	h.session.aliveEvery = 10 * time.Millisecond

	commands := make(chan media.Command, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.session.run(context.Background(), commands)
	}()

	commands <- media.TogglePlayPause
	waitFor(t, func() bool { return h.spotify.contains("pause") })

	stopped.Lock()
	running = false
	stopped.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run() went on after Soloist stopped")
	}
}

func TestRunEndsWithItsContext(t *testing.T) {
	h := newHarness(t, soWhat)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.session.run(ctx, nil)
	}()

	waitFor(t, func() bool { return h.spotify.contains("playback") })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run() went on after its context ended")
	}
}

func (f *fakeSpotify) contains(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
