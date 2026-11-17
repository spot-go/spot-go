package background

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
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
	// revoked is how a request fails once the user removed spot-go's access:
	// the token refresh fails inside the HTTP client.
	revoked = &url.Error{Op: "Get", URL: "https://api.spotify.com/v1/me/player", Err: &oauth2.RetrieveError{
		ErrorCode:        "invalid_grant",
		ErrorDescription: "Refresh token revoked",
	}}
)

// fakeSpotify is Spotify for a session. It reports state, after the answers
// it was given for the next polls. Pause and resume change state only with
// follows, as Spotify can take a moment to report them.
type fakeSpotify struct {
	mu      sync.Mutex
	state   spotify.Playback
	answers []spotify.Playback
	follows bool
	// skipTo is what plays after a skip, once Spotify reported lag first.
	skipTo *spotify.Playback
	lag    []spotify.Playback
	// err fails every request, and commandErr the commands.
	err        error
	commandErr error
	// timeout is how long the last read could take.
	timeout time.Duration
	calls   []string
}

func (f *fakeSpotify) Playback(ctx context.Context) (spotify.Playback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "playback")
	if deadline, ok := ctx.Deadline(); ok {
		f.timeout = time.Until(deadline)
	}
	if f.err != nil {
		return spotify.Playback{}, f.err
	}
	if len(f.answers) > 0 {
		answer := f.answers[0]
		f.answers = f.answers[1:]
		return answer, nil
	}
	return f.state, nil
}

func (f *fakeSpotify) Pause(context.Context) error {
	return f.command("pause", func() {
		if f.follows {
			f.state.Playing = false
		}
	})
}

func (f *fakeSpotify) ResumePlayback(_ context.Context, device libspotify.ID) error {
	return f.command("resume "+string(device), func() {
		if f.follows {
			f.state.Playing = true
		}
	})
}

func (f *fakeSpotify) Next(context.Context) error {
	return f.command("next", f.skip)
}

func (f *fakeSpotify) Previous(context.Context) error {
	return f.command("previous", f.skip)
}

func (f *fakeSpotify) skip() {
	f.answers = append(f.answers, f.lag...)
	if f.skipTo != nil {
		f.state = *f.skipTo
	}
}

func (f *fakeSpotify) command(name string, change func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, name)
	if f.err != nil {
		return f.err
	}
	if f.commandErr != nil {
		return f.commandErr
	}
	change()
	return nil
}

func (f *fakeSpotify) takeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	calls := f.calls
	f.calls = nil
	return calls
}

func (f *fakeSpotify) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Contains(f.calls, call)
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
	h.t.Helper()

	next, err := h.session.poll(context.Background())
	if err != nil {
		h.t.Fatalf("poll() = %v", err)
	}
	return next
}

func (h *harness) press(c media.Command) {
	h.t.Helper()

	next, err := h.session.command(context.Background(), c)
	if err != nil {
		h.t.Fatalf("command(%d) = %v", c, err)
	}
	if next != skipEvery {
		h.t.Fatalf("next poll in %v after a key, want %v", next, skipEvery)
	}
}

// wantShown checks what Now Playing got since the last check.
func (h *harness) wantShown(want ...media.Info) {
	h.t.Helper()

	h.mu.Lock()
	got := h.published
	h.published = nil
	h.mu.Unlock()
	if !slices.Equal(got, want) {
		h.t.Fatalf("published %+v, want %+v", got, want)
	}
}

func (h *harness) wantCalls(want ...string) {
	h.t.Helper()

	if got := h.spotify.takeCalls(); !slices.Equal(got, want) {
		h.t.Fatalf("Spotify got %q, want %q", got, want)
	}
}

func (h *harness) wantNextPoll(got, want time.Duration) {
	h.t.Helper()

	if got != want {
		h.t.Fatalf("next poll in %v, want %v", got, want)
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

	h.wantNextPoll(h.poll(), playingEvery)
	h.wantCalls("playback")
	h.wantShown(shown(soWhat, time.Minute, true))
	if h.spotify.timeout <= 0 || h.spotify.timeout > pollTimeout {
		t.Fatalf("the poll could take %v, want at most %v, as keys wait for it", h.spotify.timeout, pollTimeout)
	}
}

func TestPollWhenTheSongEnds(t *testing.T) {
	ending := soWhat
	ending.Progress = ending.Duration - 2*time.Second
	h := newHarness(t, ending)

	// A second after the song ends.
	h.wantNextPoll(h.poll(), 3*time.Second)
}

func TestPollWhilePaused(t *testing.T) {
	paused := soWhat
	paused.Playing = false
	h := newHarness(t, paused)

	h.wantNextPoll(h.poll(), pausedEvery)
	h.wantShown(shown(soWhat, time.Minute, false))
}

func TestPollKeepsTheLastSongPausedWhenNothingPlays(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	// Spotify has no active device a while after a pause.
	h.spotify.state = spotify.Playback{}
	h.advance(10 * time.Second)
	h.wantNextPoll(h.poll(), pausedEvery)
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

func TestPollBacksOffWhileItFails(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.err = errors.New("offline")

	for _, want := range []time.Duration{pausedEvery, 2 * pausedEvery, 4 * pausedEvery, maxFailedEvery, maxFailedEvery} {
		h.wantNextPoll(h.poll(), want)
	}
	for range 100 {
		h.poll()
	}
	h.wantNextPoll(h.poll(), maxFailedEvery)
	h.wantShown()

	h.spotify.err = nil
	h.wantNextPoll(h.poll(), playingEvery)
	h.spotify.err = errors.New("offline")
	h.wantNextPoll(h.poll(), pausedEvery)
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

			h.press(tt.key)
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

	h.advance(time.Second)
	h.press(media.TogglePlayPause)
	h.wantCalls("pause")
	h.wantShown(shown(soWhat, time.Minute+time.Second, false))

	// While Spotify doesn't follow, the keys go by what Now Playing shows.
	h.advance(followGrace + time.Second)
	h.wantNextPoll(h.poll(), settleEvery)
	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "resume ")
	h.wantShown(shown(soWhat, time.Minute+time.Second, true))

	// Once Spotify agrees, they go by what it says.
	h.advance(followGrace)
	h.wantNextPoll(h.poll(), playingEvery)
	h.wantShown(shown(soWhat, time.Minute, true))
	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "playback", "pause")
}

func TestKeyAfterAPlaySpotifyIsSlowToReport(t *testing.T) {
	h := newHarness(t, spotify.Playback{})
	h.session.shown, h.session.shownAt = soWhat, h.clock
	h.session.shown.Playing = false
	h.spotify.follows = false

	h.press(media.Play)
	h.wantCalls("playback", "resume "+string(soloistDevice))
	h.wantShown(shown(soWhat, time.Minute, true))
	h.advance(followGrace)
	h.wantNextPoll(h.poll(), settleEvery)
	h.advance(1500 * time.Millisecond)

	// Now Playing shows the play, so pause pauses, though Spotify doesn't
	// report the play yet.
	h.spotify.takeCalls()
	h.press(media.Pause)
	h.wantCalls("pause")
	h.wantShown(shown(soWhat, time.Minute+4500*time.Millisecond, false))
}

func TestPollAfterASkipRightAfterAPlayPause(t *testing.T) {
	h := newHarness(t, soWhat)
	h.press(media.TogglePlayPause)
	h.spotify.skipTo = &freddie
	h.press(media.NextTrack)
	h.spotify.takeCalls()
	h.wantShown(shown(soWhat, time.Minute, false))

	// The skip ends the wait for the pause to show.
	h.wantNextPoll(h.poll(), playingEvery)
	h.wantCalls("playback")
	h.wantShown(shown(freddie, 0, true))
}

func TestPollRightAfterAPlayPauseWaitsForSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.press(media.TogglePlayPause)
	h.spotify.takeCalls()
	h.wantShown(shown(soWhat, time.Minute, false))

	h.advance(time.Second)
	h.wantNextPoll(h.poll(), followGrace-time.Second)
	h.wantCalls()
	h.wantShown()

	h.advance(followGrace)
	h.wantNextPoll(h.poll(), pausedEvery)
	h.wantCalls("playback")
	h.wantShown(shown(soWhat, time.Minute, false))
}

func TestPollWaitsForSpotifyToPlayAfterALongPause(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.spotify.state = spotify.Playback{}
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true), shown(soWhat, time.Minute, false))

	// Soloist takes a few seconds to play again: until then, Spotify has
	// no active device.
	h.spotify.follows = false
	h.press(media.Play)
	h.wantShown(shown(soWhat, time.Minute, true))
	h.advance(followGrace)
	h.wantNextPoll(h.poll(), settleEvery)
	h.advance(settleEvery)
	h.wantNextPoll(h.poll(), settleEvery)
	h.wantShown()

	h.spotify.state = soWhat
	h.advance(settleEvery)
	h.wantNextPoll(h.poll(), playingEvery)
	h.wantShown(shown(soWhat, time.Minute, true))
}

func TestPollShowsWhatSpotifySaysOnceItDoesntFollowAPlay(t *testing.T) {
	paused := soWhat
	paused.Playing = false
	h := newHarness(t, paused)
	h.spotify.follows = false
	h.press(media.Play)
	h.wantShown(shown(soWhat, time.Minute, true))

	h.advance(settleTimeout - time.Second)
	h.wantNextPoll(h.poll(), settleEvery)
	h.wantShown()
	h.advance(time.Second)
	h.wantNextPoll(h.poll(), pausedEvery)
	h.wantShown(shown(soWhat, time.Minute, false))
}

func TestSkipWaitsForSpotifyToPlayTheNextSong(t *testing.T) {
	for _, key := range []media.Command{media.NextTrack, media.PreviousTrack} {
		h := newHarness(t, soWhat)
		h.poll()
		h.wantShown(shown(soWhat, time.Minute, true))
		h.spotify.takeCalls()

		h.spotify.skipTo, h.spotify.lag = &freddie, []spotify.Playback{soWhat, {}}
		h.press(key)
		for range 2 {
			// Spotify still plays the song before, and then reports
			// nothing for a moment.
			h.wantNextPoll(h.poll(), skipEvery)
		}
		h.wantShown()
		h.wantNextPoll(h.poll(), playingEvery)
		h.wantShown(shown(freddie, 0, true))
		h.wantCalls("playback", map[media.Command]string{media.NextTrack: "next", media.PreviousTrack: "previous"}[key], "playback", "playback", "playback")
	}
}

func TestSkipToTheStartOfTheSameSong(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	restarted := soWhat
	restarted.Progress = time.Second
	h.spotify.skipTo = &restarted
	h.advance(time.Second)
	h.press(media.PreviousTrack)
	h.wantNextPoll(h.poll(), playingEvery)
	h.wantShown(shown(soWhat, time.Second, true))
}

func TestSkipTellsFromWhatSpotifyReportedBeforeIt(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	// Spotify plays behind what Now Playing counts, as when Soloist took a
	// moment to play: right before and right after the skip, it reports the
	// same song where it was.
	h.advance(10 * time.Second)
	h.spotify.skipTo, h.spotify.lag = &freddie, []spotify.Playback{soWhat}
	h.press(media.NextTrack)
	h.wantNextPoll(h.poll(), skipEvery)
	h.wantShown()
	h.poll()
	h.wantShown(shown(freddie, 0, true))
}

func TestSkipStopsWaitingForSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.poll()
	h.wantShown(shown(soWhat, time.Minute, true))

	h.press(media.NextTrack)
	for range skipPolls - 1 {
		h.wantNextPoll(h.poll(), skipEvery)
	}
	h.wantShown()
	h.wantNextPoll(h.poll(), playingEvery)
	h.wantShown(shown(soWhat, time.Minute, true))
}

func TestPlayPauseAfterASkipAsksSpotify(t *testing.T) {
	h := newHarness(t, soWhat)
	h.press(media.TogglePlayPause)
	h.spotify.takeCalls()

	// The pause it knows of doesn't hold after a skip.
	h.spotify.skipTo = &freddie
	h.press(media.NextTrack)
	h.press(media.TogglePlayPause)
	h.wantCalls("playback", "next", "playback", "pause")
}

func TestFailedKeys(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.commandErr = errors.New("Restriction violated")
	h.press(media.TogglePlayPause)
	h.press(media.NextTrack)
	h.wantCalls("playback", "pause", "playback", "next")
	h.wantShown()

	// A key that can't read what plays does nothing.
	h.spotify.err = errors.New("offline")
	h.press(media.TogglePlayPause)
	h.press(media.PreviousTrack)
	h.wantCalls("playback", "playback")
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
	h.press(media.TogglePlayPause)
	h.spotify.err = nil
	h.poll()
	h.spotify.err = errors.New("offline")
	h.poll()

	if n := strings.Count(out.String(), "offline"); n != 2 {
		t.Fatalf("logged %d errors, want 2:\n%s", n, out.String())
	}
}

func TestOtherLoginErrorsDontEndTheSession(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.err = &url.Error{Op: "Get", URL: "https://api.spotify.com/v1/me/player", Err: &oauth2.RetrieveError{ErrorCode: "server_error"}}

	h.wantNextPoll(h.poll(), pausedEvery)
	h.press(media.TogglePlayPause)
	h.spotify.err = &url.Error{Op: "Get", URL: "https://api.spotify.com/v1/me/player", Err: &oauth2.RetrieveError{}}
	h.wantNextPoll(h.poll(), 2*pausedEvery)
}

func TestLoggedOutEndsTheSession(t *testing.T) {
	h := newHarness(t, soWhat)
	h.spotify.err = revoked

	if _, err := h.session.poll(context.Background()); !errors.Is(err, errLoggedOut) {
		t.Fatalf("poll() = %v, want %v", err, errLoggedOut)
	}
	for _, key := range []media.Command{media.TogglePlayPause, media.NextTrack} {
		if _, err := h.session.command(context.Background(), key); !errors.Is(err, errLoggedOut) {
			t.Fatalf("command(%d) = %v, want %v", key, err, errLoggedOut)
		}
	}
	if err := h.session.run(context.Background(), nil); !errors.Is(err, errLoggedOut) {
		t.Fatalf("run() = %v, want %v", err, errLoggedOut)
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
	done := make(chan error)
	go func() { done <- h.session.run(context.Background(), commands) }()

	commands <- media.TogglePlayPause
	waitFor(t, func() bool { return h.spotify.called("pause") })

	stopped.Lock()
	running = false
	stopped.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run() went on after Soloist stopped")
	}
}

func TestRunPollsOnlyWhenItIsTime(t *testing.T) {
	h := newHarness(t, soWhat)
	h.session.now = time.Now
	h.session.aliveEvery = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- h.session.run(ctx, nil) }()

	// Many checks that Soloist runs, and a single poll.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if calls := h.spotify.takeCalls(); len(calls) != 1 {
		t.Fatalf("Spotify got %q in 200ms, want one poll", calls)
	}
}

func TestRunEndsWithItsContext(t *testing.T) {
	h := newHarness(t, soWhat)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- h.session.run(ctx, nil) }()

	waitFor(t, func() bool { return h.spotify.called("playback") })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run() went on after its context ended")
	}
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
