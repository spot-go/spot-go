package background

import (
	"context"
	"log"
	"time"

	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
)

const (
	// playingEvery is how often Spotify is asked what plays, as the player
	// does, and pausedEvery how often while nothing plays.
	playingEvery = 5 * time.Second
	pausedEvery  = 15 * time.Second
	// aliveEvery is how often spot-go checks that the Soloist it follows
	// still runs.
	aliveEvery = time.Second
	// skipEvery is how often, and skipPolls how many times, Spotify is asked
	// what plays after a skip, until it plays something else.
	skipEvery = 500 * time.Millisecond
	skipPolls = 6
	// followGrace is how long Spotify can still report what played before a
	// play or pause.
	followGrace = 3 * time.Second
	// pollSlack is how long after a song ends Spotify is asked what plays
	// next.
	pollSlack      = time.Second
	requestTimeout = 15 * time.Second
	commandQueue   = 4
)

type (
	// remote is what the session uses of the Spotify Web API.
	remote interface {
		Playback(ctx context.Context) (spotify.Playback, error)
		Pause(ctx context.Context) error
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
		// media key left it, at changedAt.
		changed   spotify.Playback
		changedAt time.Time
		// skippedFrom is what played before a skip, and where, while
		// skipPolls more polls wait for Spotify to play something else.
		skippedFrom spotify.Playback
		skipPolls   int
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
// Soloist stops or ctx ends.
func (s *session) run(ctx context.Context, commands <-chan media.Command) {
	poll := time.NewTimer(0)
	defer poll.Stop()
	alive := time.NewTicker(s.aliveEvery)
	defer alive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-alive.C:
			if !s.running() {
				return
			}
		case c := <-commands:
			if next, ok := s.command(ctx, c); ok {
				poll.Reset(next)
			}
		case <-poll.C:
			poll.Reset(s.poll(ctx))
		}
	}
}

// poll shows what Spotify plays, and returns when to poll next.
func (s *session) poll(ctx context.Context) time.Duration {
	if wait := s.changedAt.Add(followGrace).Sub(s.now()); !s.changedAt.IsZero() && wait > 0 {
		// Spotify may still report what played before the play or pause.
		return wait
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	state, err := s.spotify.Playback(ctx)
	if err != nil {
		s.fail("read what Spotify plays", err)
		return s.pausedEvery
	}
	s.failure = ""

	if s.skipPolls > 0 {
		s.skipPolls--
		if s.skipPolls > 0 && !s.skipped(state) {
			return s.skipEvery
		}
		s.skipPolls = 0
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
	return s.nextPoll(state)
}

// skipped reports whether state shows the skip took place: something else
// plays, or the same song from its start again.
func (s *session) skipped(state spotify.Playback) bool {
	from := s.skippedFrom
	return state.Item != from.Item || state.Progress < from.Progress
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

// command runs a media key. It returns when to poll next, or false to keep
// the time of the next poll.
func (s *session) command(ctx context.Context, c media.Command) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	switch c {
	case media.TogglePlayPause, media.Play, media.Pause:
		return s.playPause(ctx, c), true
	case media.NextTrack, media.PreviousTrack:
		return s.skip(ctx, c == media.NextTrack), true
	}
	return 0, false
}

func (s *session) playPause(ctx context.Context, c media.Command) time.Duration {
	state, err := s.state(ctx)
	if err != nil {
		s.fail("read what Spotify plays", err)
		return s.skipEvery
	}

	pause := state.Playing
	switch c {
	case media.Play:
		pause = false
	case media.Pause:
		pause = true
	}
	switch {
	case pause && state.Playing:
		err = s.spotify.Pause(ctx)
	case !pause && !state.Playing:
		var target libspotify.ID
		if state.Device == "" {
			// With no active device, Spotify plays only where it is told.
			target, state.Device = s.device, s.device
		}
		err = s.spotify.ResumePlayback(ctx, target)
	}
	if err != nil {
		s.fail("play or pause", err)
		return s.skipEvery
	}
	s.failure = ""

	state.Playing = !pause
	now := s.now()
	s.changed, s.changedAt = state, now
	if state.Item == "" {
		last, ok := s.last(state.Playing)
		if !ok {
			return followGrace
		}
		last.Device = state.Device
		state = last
	}
	s.show(state)
	return followGrace
}

// state is what Spotify plays: as the last play or pause left it, when it
// ended a moment ago, as Spotify may not report it yet, or as Spotify says.
func (s *session) state(ctx context.Context) (spotify.Playback, error) {
	if since := s.now().Sub(s.changedAt); !s.changedAt.IsZero() && since < followGrace {
		return at(s.changed, since), nil
	}
	return s.spotify.Playback(ctx)
}

func (s *session) skip(ctx context.Context, next bool) time.Duration {
	skip := s.spotify.Previous
	if next {
		skip = s.spotify.Next
	}
	if err := skip(ctx); err != nil {
		s.fail("skip", err)
		return s.skipEvery
	}
	s.failure = ""

	s.skippedFrom = at(s.shown, s.now().Sub(s.shownAt))
	s.skipPolls = skipPolls
	// What a play or pause left changed with the skip.
	s.changedAt = time.Time{}
	return s.skipEvery
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
