package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

type (
	// artworks shows what plays in Now Playing with its artwork, which it
	// fetches from Info.Artwork. Now Playing on macOS takes the image
	// itself, not its URL. Until the image arrives, Now Playing shows what
	// plays without one.
	artworks struct {
		// fetch gets the image at a URL.
		fetch func(ctx context.Context, url string) ([]byte, error)
		// show shows info in Now Playing, with image as its artwork, or none
		// when image is nil.
		show func(info Info, image []byte)
		now  func() time.Time

		mu sync.Mutex
		// latest is what was published last, at latestAt.
		latest   Info
		latestAt time.Time
		// kept are the images fetched last, the newest last.
		kept     []artwork
		fetching map[string]bool
		// failed is when fetching an image failed last, by its URL.
		failed map[string]time.Time
	}

	artwork struct {
		url   string
		image []byte
	}
)

const (
	// artworkTimeout is how long fetching an image can take.
	artworkTimeout = 20 * time.Second
	// maxArtworkBytes is how big an image can be. Spotify's take tens of
	// kilobytes.
	maxArtworkBytes = 4 << 20
	// keptArtworks is how many images are kept, so a song played again, as
	// after skipping back, shows its artwork at once.
	keptArtworks = 8
	// artworkRetry is how long after failing, as while offline, an image is
	// fetched again.
	artworkRetry = time.Minute
)

func newArtworks(show func(Info, []byte)) *artworks {
	client := &http.Client{Timeout: artworkTimeout}
	return &artworks{
		fetch:    func(ctx context.Context, url string) ([]byte, error) { return fetchArtwork(ctx, client, url) },
		show:     show,
		now:      time.Now,
		fetching: map[string]bool{},
		failed:   map[string]time.Time{},
	}
}

// publish shows info in Now Playing, with its artwork when it has it
// already. Otherwise, it fetches the artwork, and shows it once it arrives,
// if info is still what plays.
func (a *artworks) publish(info Info) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.latest, a.latestAt = info, a.now()
	image, ok := a.image(info.Artwork)
	a.show(info, image)
	if !ok {
		a.load(info.Artwork)
	}
}

// image is the image kept for url.
func (a *artworks) image(url string) ([]byte, bool) {
	i := slices.IndexFunc(a.kept, func(k artwork) bool { return k.url == url })
	if url == "" || i < 0 {
		return nil, false
	}
	kept := a.kept[i]
	a.kept = append(slices.Delete(a.kept, i, i+1), kept)
	return kept.image, true
}

func (a *artworks) keep(url string, image []byte) {
	a.kept = append(a.kept, artwork{url: url, image: image})
	if over := len(a.kept) - keptArtworks; over > 0 {
		a.kept = slices.Delete(a.kept, 0, over)
	}
}

// load fetches the image at url, unless it is fetched already or failed a
// moment ago.
func (a *artworks) load(url string) {
	now := a.now()
	for u, at := range a.failed {
		if now.Sub(at) >= artworkRetry {
			delete(a.failed, u)
		}
	}
	if _, failed := a.failed[url]; url == "" || failed || a.fetching[url] {
		return
	}
	a.fetching[url] = true
	go a.loaded(url)
}

func (a *artworks) loaded(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), artworkTimeout)
	defer cancel()
	image, err := a.fetch(ctx, url)

	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.fetching, url)
	if err != nil {
		a.failed[url] = a.now()
		return
	}
	a.keep(url, image)
	if a.latest.Artwork == url {
		a.show(a.latest.later(a.now().Sub(a.latestAt)), image)
	}
}

func fetchArtwork(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fail to fetch the artwork: HTTP %d", resp.StatusCode)
	}
	image, err := io.ReadAll(io.LimitReader(resp.Body, maxArtworkBytes+1))
	switch {
	case err != nil:
		return nil, err
	case len(image) > maxArtworkBytes:
		return nil, fmt.Errorf("the artwork takes more than %d bytes", maxArtworkBytes)
	case len(image) == 0:
		return nil, fmt.Errorf("the artwork is empty")
	}
	return image, nil
}
