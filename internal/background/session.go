package background

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

const (
	// playingEvery is how often Spotify is asked what plays, as the player
	// does, and pausedEvery how often while nothing plays.
	playingEvery = 5 * time.Second
	pausedEvery  = 15 * time.Second
	// maxFailedEvery is how long spot-go waits at most between polls that
	// fail, as while offline: each failure doubles the wait from
	// pausedEvery.
	maxFailedEvery = 2 * time.Minute
	// aliveEvery is how often spot-go checks that the Soloist it follows
	// still runs.
	aliveEvery = time.Second
	// skipEvery is how often, and skipPolls how many times, Spotify is asked
	// what plays after a skip, until it plays something else.
	skipEvery = 500 * time.Millisecond
	skipPolls = 6
	// followGrace is how long Spotify can still report what played before a
	// play or pause. Polls after it wait settleEvery, up to settleTimeout
	// after the key, while Spotify reports nothing, as when Soloist takes a
	// few seconds to play again after a long pause.
	followGrace   = 3 * time.Second
	settleEvery   = time.Second
	settleTimeout = 10 * time.Second
	// pollSlack is how long after a song ends Spotify is asked what plays
	// next.
	pollSlack = time.Second
	// pollTimeout is short, as a media key pressed meanwhile waits for the
	// poll to end. A token refresh can take longer: see refreshTimeout.
	pollTimeout    = 5 * time.Second
	commandTimeout = 15 * time.Second
	commandQueue   = 4
)

var errLoggedOut = errors.New("spot-go needs to log in to Spotify again: run spot-go")

type (
	// remote is what the session uses of the Spotify Web API.
	remote interface {
		Playback(ctx context.Context) (spotify.Playback, error)
		PauseOn(ctx context.Context, deviceID libspotify.ID) error
		ResumePlayback(ctx context.Context, deviceID libspotify.ID) error
		Next(ctx context.Context) error
		Previous(ctx context.Context) error
	}

	// session shows what Spotify plays in Now Playing, and runs the media
	// keys, while spot-go is in the background.
	session struct {
		spotify remote
		// device is Soloist's device, where a play goes when Spotify has no
		// active device.
		device  libspotify.ID
		publish func(media.Info)
		// running reports whether the Soloist spot-go left playing still
		// runs.
		running func() bool
		now     func() time.Time

		playingEvery, pausedEvery, aliveEvery, skipEvery time.Duration

		// shown is what Now Playing shows, as Spotify reported it at shownAt.
		shown   spotify.Playback
		shownAt time.Time
		// changed is what Spotify plays as the last play or pause from a
		// media key left it, at changedAt. settling is true until a poll
		// agrees with it, or settleTimeout passed.
		changed   spotify.Playback
		changedAt time.Time
		settling  bool
		// skippedFrom is what Spotify played right before a skip, while
		// skipPolls more polls wait for it to play something else.
		skippedFrom spotify.Playback
		skipPolls   int
		// failures counts the polls that failed in a row.
		failures int
		// failure is the error logged last, so an error that repeats, as
		// while offline, is logged once.
		failure string
	}
)

func newSession(r remote, device libspotify.ID, publish func(media.Info), running func() bool) *session {
	return &session{
		spotify:      r,
		device:       device,
		publish:      publish,
		running:      running,
		now:          time.Now,
		playingEvery: playingEvery,
		pausedEvery:  pausedEvery,
		aliveEvery:   aliveEvery,
		skipEvery:    skipEvery,
	}
}

// run runs the media keys from commands, and polls Spotify, until the
// Soloist stops or ctx ends. It fails when Spotify logged spot-go out.
func (s *session) run(ctx context.Context, commands <-chan media.Command) error {
	poll := time.NewTimer(0)
	defer poll.Stop()
	alive := time.NewTicker(s.aliveEvery)
	defer alive.Stop()

	for {
		var (
			next time.Duration
			err  error
		)
		select {
		case <-ctx.Done():
			return nil
		case <-alive.C:
			if !s.running() {
				return nil
			}
			continue
		case c := <-commands:
			next, err = s.command(ctx, c)
		case <-poll.C:
			next, err = s.poll(ctx)
		}
		if err != nil {
			return err
		}
		poll.Reset(next)
	}
}

// poll shows what Spotify plays, and returns when to poll next.
func (s *session) poll(ctx context.Context) (time.Duration, error) {
	if wait := s.changedAt.Add(followGrace).Sub(s.now()); s.settling && wait > 0 {
		// Spotify may still report what played before the play or pause.
		return wait, nil
	}

	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	state, err := s.spotify.Playback(ctx)
	if err != nil {
		if loggedOut(err) {
			return 0, errLoggedOut
		}
		s.fail("read what Spotify plays", err)
		s.failures = min(s.failures+1, 8)
		return min(s.pausedEvery<<(s.failures-1), maxFailedEvery), nil
	}
	s.failures, s.failure = 0, ""

	if s.skipPolls > 0 {
		s.skipPolls--
		if s.skipPolls > 0 && !s.skipped(state) {
			return s.skipEvery, nil
		}
		s.skipPolls = 0
	}
	if s.settling {
		// Spotify reports nothing while Soloist starts to play again after a
		// long pause, and a device that plays, or not, once it does.
		if state.Item == "" && state.Playing != s.changed.Playing && s.now().Sub(s.changedAt) < settleTimeout {
			return settleEvery, nil
		}
		s.settling = false
	}

	switch {
	case state.Item != "":
		s.show(state)
	case state.Playing:
		// Spotify plays something it doesn't name: Now Playing keeps what it
		// showed.
	default:
		// Nothing plays, or no device is active, as a while after a pause:
		// Now Playing keeps the last song, paused, so the play key resumes
		// it.
		if last, ok := s.last(false); ok {
			s.show(last)
		}
	}
	return s.nextPoll(state), nil
}

// skipped reports whether state shows the skip took place: something else
// plays, or the same song from its start again. Spotify can report nothing
// in between.
func (s *session) skipped(state spotify.Playback) bool {
	from := s.skippedFrom
	return state.Item != "" && (state.Item != from.Item || state.Progress < from.Progress)
}

func (s *session) nextPoll(state spotify.Playback) time.Duration {
	if !state.Playing {
		return s.pausedEvery
	}
	if left := state.Duration - state.Progress; left > 0 {
		return min(s.playingEvery, left+pollSlack)
	}
	return s.playingEvery
}

// command runs a media key, and returns when to poll next.
func (s *session) command(ctx context.Context, c media.Command) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	var err error
	switch c {
	case media.TogglePlayPause, media.Play, media.Pause:
		err = s.playPause(ctx, c)
	case media.NextTrack, media.PreviousTrack:
		err = s.skip(ctx, c == media.NextTrack)
	}
	switch {
	case err == nil:
		s.failure = ""
	case loggedOut(err):
		return 0, errLoggedOut
	default:
		s.fail("run a media key", err)
	}
	// After a play or pause, the poll waits for the grace to end.
	return s.skipEvery, nil
}

func (s *session) playPause(ctx context.Context, c media.Command) error {
	state, err := s.state(ctx)
	if err != nil {
		return err
	}

	pause := state.Playing
	switch c {
	case media.Play:
		pause = false
	case media.Pause:
		pause = true
	}
	sent := true
	switch {
	case pause && state.Playing:
		// A play Spotify doesn't report yet has no active device: the
		// pause goes where the play went.
		err = s.spotify.PauseOn(ctx, state.Device)
	case !pause && !state.Playing:
		var target libspotify.ID
		if state.Device == "" {
			// With no active device, Spotify plays only where it is told.
			target, state.Device = s.device, s.device
		}
		err = s.spotify.ResumePlayback(ctx, target)
	default:
		sent = false
	}
	if err != nil {
		return err
	}

	state.Playing = !pause
	if sent {
		s.changed, s.changedAt, s.settling = state, s.now(), true
	}
	if state.Item == "" {
		last, ok := s.last(state.Playing)
		if !ok {
			return nil
		}
		last.Device = state.Device
		state = last
	}
	s.show(state)
	return nil
}

// state is what Spotify plays: as the last play or pause left it, while
// Spotify doesn't report it yet and Now Playing shows it, or as Spotify says.
func (s *session) state(ctx context.Context) (spotify.Playback, error) {
	if since := s.now().Sub(s.changedAt); s.settling && since < settleTimeout {
		return at(s.changed, since), nil
	}
	return s.spotify.Playback(ctx)
}

func (s *session) skip(ctx context.Context, next bool) error {
	// What plays right before the skip, as Spotify says, tells when it
	// took place.
	before, err := s.spotify.Playback(ctx)
	if err != nil {
		return err
	}
	skip := s.spotify.Previous
	if next {
		skip = s.spotify.Next
	}
	if err := skip(ctx); err != nil {
		return err
	}

	s.skippedFrom, s.skipPolls = before, skipPolls
	// What a play or pause left changed with the skip.
	s.settling = false
	return nil
}

// last is the song Now Playing shows, where it is now, playing or not.
func (s *session) last(playing bool) (spotify.Playback, bool) {
	if s.shown.Item == "" {
		return spotify.Playback{}, false
	}
	last := at(s.shown, s.now().Sub(s.shownAt))
	last.Playing = playing
	return last, true
}

func (s *session) show(state spotify.Playback) {
	s.shown, s.shownAt = state, s.now()
	s.publish(media.Info{
		ID:       string(state.Item),
		Title:    state.Name,
		Artist:   state.Artist,
		Artwork:  state.Artwork,
		Duration: state.Duration,
		Elapsed:  state.Progress,
		Playing:  state.Playing,
	})
}

func (s *session) fail(what string, err error) {
	if msg := err.Error(); msg != s.failure {
		s.failure = msg
		log.Printf("fail to %s: %v", what, err)
	}
}

// loggedOut reports whether err is Spotify refusing the saved login, as
// when the user removed spot-go's access: only a new login in spot-go helps.
func loggedOut(err error) bool {
	var retrieve *oauth2.RetrieveError
	return errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_grant"
}

// at is state since later, when it plays on.
func at(state spotify.Playback, since time.Duration) spotify.Playback {
	if state.Playing {
		state.Progress += since
		if state.Duration > 0 {
			state.Progress = min(state.Progress, state.Duration)
		}
	}
	return state
}
