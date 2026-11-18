package media

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	kindOfBlue = "https://i.scdn.co/image/kind-of-blue"
	abbeyRoad  = "https://i.scdn.co/image/abbey-road"
)

type (
	// artworksHarness runs artworks with a fake fetch, which waits for the
	// test to answer, and records what Now Playing shows.
	artworksHarness struct {
		t        *testing.T
		artworks *artworks
		clock    time.Time
		fetches  chan fetch

		mu    sync.Mutex
		shown []shownInfo
	}

	fetch struct {
		url    string
		answer chan fetched
	}

	fetched struct {
		image []byte
		err   error
	}

	shownInfo struct {
		info  Info
		image string
	}
)

func newArtworksHarness(t *testing.T) *artworksHarness {
	h := &artworksHarness{t: t, clock: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), fetches: make(chan fetch, 16)}
	h.artworks = newArtworks(h.show)
	h.artworks.now = h.now
	h.artworks.fetch = func(ctx context.Context, url string) ([]byte, error) {
		answer := make(chan fetched, 1)
		h.fetches <- fetch{url: url, answer: answer}
		select {
		case a := <-answer:
			return a.image, a.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return h
}

func (h *artworksHarness) now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.clock
}

func (h *artworksHarness) advance(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clock = h.clock.Add(d)
}

func (h *artworksHarness) show(info Info, image []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.shown = append(h.shown, shownInfo{info: info, image: string(image)})
}

// fetched waits for a fetch of url.
func (h *artworksHarness) fetched(url string) fetch {
	h.t.Helper()

	select {
	case f := <-h.fetches:
		if f.url != url {
			h.t.Fatalf("fetched %q, want %q", f.url, url)
		}
		return f
	case <-time.After(time.Second):
		h.t.Fatalf("%q was not fetched", url)
		return fetch{}
	}
}

func (h *artworksHarness) wantNoFetch() {
	h.t.Helper()

	select {
	case f := <-h.fetches:
		h.t.Fatalf("fetched %q, want no fetch", f.url)
	default:
	}
	// A fetch that just started may not have reached h.fetches yet, but
	// load notes it at once.
	h.artworks.mu.Lock()
	fetching := len(h.artworks.fetching)
	h.artworks.mu.Unlock()
	if fetching != 0 {
		h.t.Fatalf("fetching %d, want no fetch", fetching)
	}
}

// answer answers f, and waits for artworks to take the answer.
func (h *artworksHarness) answer(f fetch, image string, err error) {
	h.t.Helper()

	f.answer <- fetched{image: []byte(image), err: err}
	deadline := time.Now().Add(time.Second)
	for {
		h.artworks.mu.Lock()
		fetching := h.artworks.fetching[f.url]
		h.artworks.mu.Unlock()
		if !fetching {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the fetch of %q didn't end", f.url)
		}
		time.Sleep(time.Millisecond)
	}
}

// wantShown checks what Now Playing showed since the last check.
func (h *artworksHarness) wantShown(want ...shownInfo) {
	h.t.Helper()

	h.mu.Lock()
	got := h.shown
	h.shown = nil
	h.mu.Unlock()
	if !slices.Equal(got, want) {
		h.t.Fatalf("shown %+v, want %+v", got, want)
	}
}

func soWhatInfo(artwork string) Info {
	return Info{ID: "spotify:track:so-what", Title: "So What", Artist: "Miles Davis", Artwork: artwork, Duration: 9 * time.Minute, Elapsed: time.Minute, Playing: true}
}

func TestArtworksShowWhatHasNoArtwork(t *testing.T) {
	h := newArtworksHarness(t)

	h.artworks.publish(soWhatInfo(""))

	h.wantShown(shownInfo{info: soWhatInfo("")})
	h.wantNoFetch()
}

func TestArtworksShowTheArtworkOnceFetched(t *testing.T) {
	h := newArtworksHarness(t)

	h.artworks.publish(soWhatInfo(kindOfBlue))
	h.wantShown(shownInfo{info: soWhatInfo(kindOfBlue)})

	f := h.fetched(kindOfBlue)
	h.advance(2 * time.Second)
	h.answer(f, "blue", nil)

	// What plays moved on while the artwork was fetched.
	later := soWhatInfo(kindOfBlue)
	later.Elapsed = time.Minute + 2*time.Second
	h.wantShown(shownInfo{info: later, image: "blue"})
}

func TestArtworksKeepTheElapsedTimeOfAPause(t *testing.T) {
	h := newArtworksHarness(t)
	paused := soWhatInfo(kindOfBlue)
	paused.Playing = false

	h.artworks.publish(paused)
	f := h.fetched(kindOfBlue)
	h.advance(2 * time.Second)
	h.answer(f, "blue", nil)

	h.wantShown(shownInfo{info: paused}, shownInfo{info: paused, image: "blue"})
}

func TestArtworksFetchAnArtworkOnce(t *testing.T) {
	h := newArtworksHarness(t)

	h.artworks.publish(soWhatInfo(kindOfBlue))
	h.artworks.publish(soWhatInfo(kindOfBlue))
	f := h.fetched(kindOfBlue)
	h.answer(f, "blue", nil)
	h.wantNoFetch()
	h.wantShown(shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: soWhatInfo(kindOfBlue), image: "blue"})

	h.artworks.publish(soWhatInfo(kindOfBlue))

	h.wantShown(shownInfo{info: soWhatInfo(kindOfBlue), image: "blue"})
	h.wantNoFetch()
}

func TestArtworksDontShowTheArtworkOfWhatNoLongerPlays(t *testing.T) {
	h := newArtworksHarness(t)
	something := Info{ID: "spotify:track:something", Title: "Something", Artwork: abbeyRoad, Playing: true}

	h.artworks.publish(soWhatInfo(kindOfBlue))
	f := h.fetched(kindOfBlue)
	h.artworks.publish(something)
	abbey := h.fetched(abbeyRoad)
	h.answer(f, "blue", nil)
	h.wantShown(shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: something})

	// The artwork is kept, for when So What plays again.
	h.answer(abbey, "abbey", nil)
	h.artworks.publish(soWhatInfo(kindOfBlue))

	h.wantShown(shownInfo{info: something, image: "abbey"}, shownInfo{info: soWhatInfo(kindOfBlue), image: "blue"})
	h.wantNoFetch()
}

func TestArtworksTryAgainAWhileAfterAFailure(t *testing.T) {
	h := newArtworksHarness(t)

	h.artworks.publish(soWhatInfo(kindOfBlue))
	h.answer(h.fetched(kindOfBlue), "", errors.New("offline"))
	h.artworks.publish(soWhatInfo(kindOfBlue))
	h.wantNoFetch()

	h.advance(artworkRetry)
	h.artworks.publish(soWhatInfo(kindOfBlue))
	h.answer(h.fetched(kindOfBlue), "blue", nil)

	h.wantShown(shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: soWhatInfo(kindOfBlue)}, shownInfo{info: soWhatInfo(kindOfBlue), image: "blue"})
	if len(h.artworks.failed) != 0 {
		t.Fatalf("failed = %v, want none left", h.artworks.failed)
	}
}

func TestArtworksKeepTheLatestOnes(t *testing.T) {
	h := newArtworksHarness(t)
	url := func(i int) string { return kindOfBlue + "-" + string(rune('a'+i)) }
	for i := range keptArtworks + 1 {
		info := soWhatInfo(url(i))
		h.artworks.publish(info)
		h.answer(h.fetched(url(i)), url(i), nil)
		if i == 1 {
			// Shown again, the first artwork is newer than the second.
			h.artworks.publish(soWhatInfo(url(0)))
		}
	}
	h.wantNoFetch()

	h.artworks.publish(soWhatInfo(url(0)))
	h.wantNoFetch()
	h.artworks.publish(soWhatInfo(url(1)))
	h.answer(h.fetched(url(1)), url(1), nil)
}

func TestFetchArtwork(t *testing.T) {
	big := bytes.Repeat([]byte("x"), maxArtworkBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/kind-of-blue":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("blue"))
		case "/big":
			_, _ = w.Write(big)
		case "/empty":
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tt := range []struct {
		path, want, err string
	}{
		{"/kind-of-blue", "blue", ""},
		{"/missing", "", "HTTP 404"},
		{"/big", "", "more than"},
		{"/empty", "", "empty"},
	} {
		got, err := fetchArtwork(context.Background(), server.Client(), server.URL+tt.path)
		if string(got) != tt.want || (err == nil) != (tt.err == "") || err != nil && !strings.Contains(err.Error(), tt.err) {
			t.Errorf("fetchArtwork(%s) = %q, %v, want %q, error %q", tt.path, got, err, tt.want, tt.err)
		}
	}
}

func TestInfoLater(t *testing.T) {
	for _, tt := range []struct {
		name string
		info Info
		want time.Duration
	}{
		{"playing moves on", Info{Elapsed: time.Minute, Playing: true}, 65 * time.Second},
		{"paused stays", Info{Elapsed: time.Minute}, time.Minute},
		{"stops at the end", Info{Elapsed: time.Minute, Duration: 62 * time.Second, Playing: true}, 62 * time.Second},
		{"paused past the end", Info{Elapsed: 70 * time.Second, Duration: 62 * time.Second}, 62 * time.Second},
	} {
		if got := tt.info.later(5 * time.Second).Elapsed; got != tt.want {
			t.Errorf("%s: elapsed = %v, want %v", tt.name, got, tt.want)
		}
	}
}
