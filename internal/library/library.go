package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zmb3/spotify/v2"
)

type (
	Order string

	Playlist struct {
		AddedAt    time.Time `json:"addedAt,omitzero"`
		LastPlayed time.Time `json:"lastPlayed,omitzero"`
	}

	History struct {
		Order     Order                    `json:"order,omitempty"`
		Synced    bool                     `json:"synced,omitempty"`
		Playlists map[spotify.URI]Playlist `json:"playlists,omitempty"`
		Podcasts  Podcasts                 `json:"podcasts,omitzero"`
		revision  int
	}

	Podcasts struct {
		Order      Order                     `json:"order,omitempty"`
		LastPlayed map[spotify.URI]time.Time `json:"lastPlayed,omitempty"`
	}

	Store struct {
		path  string
		mu    sync.Mutex
		saved int
	}
)

var ErrCorrupt = errors.New("corrupt file")

const (
	OrderRecents       Order = "recents"
	OrderAlphabetical  Order = "alphabetical"
	OrderCreator       Order = "creator"
	OrderCustom        Order = "custom"
	OrderRecentlyAdded Order = "recently-added"
)

func (h *History) SetOrder(o Order) {
	if h.Order == o {
		return
	}
	h.Order = o
	h.revision++
}

func (h *History) Sync(uris []spotify.URI, now time.Time) {
	changed := !h.Synced
	if h.Playlists == nil {
		h.Playlists = make(map[spotify.URI]Playlist, len(uris))
	}

	present := make(map[spotify.URI]bool, len(uris))
	for _, uri := range uris {
		present[uri] = true
		if _, ok := h.Playlists[uri]; ok {
			continue
		}
		var p Playlist
		if h.Synced {
			p.AddedAt = now
		}
		h.Playlists[uri] = p
		changed = true
	}
	for uri := range h.Playlists {
		if !present[uri] {
			delete(h.Playlists, uri)
			changed = true
		}
	}

	h.Synced = true
	if changed {
		h.revision++
	}
}

func (h *History) Played(uri spotify.URI, at time.Time) bool {
	p, ok := h.Playlists[uri]
	if !ok || !at.After(p.LastPlayed) {
		return false
	}
	p.LastPlayed = at
	h.Playlists[uri] = p
	h.revision++
	return true
}

func (h History) Recent(uri spotify.URI) time.Time {
	p := h.Playlists[uri]
	if p.LastPlayed.After(p.AddedAt) {
		return p.LastPlayed
	}
	return p.AddedAt
}

func (h *History) SetPodcastOrder(o Order) {
	if h.Podcasts.Order == o {
		return
	}
	h.Podcasts.Order = o
	h.revision++
}

func (h *History) PodcastPlayed(uri spotify.URI, at time.Time) bool {
	if !at.After(h.Podcasts.LastPlayed[uri]) {
		return false
	}
	if h.Podcasts.LastPlayed == nil {
		h.Podcasts.LastPlayed = make(map[spotify.URI]time.Time)
	}
	h.Podcasts.LastPlayed[uri] = at
	h.revision++
	return true
}

func (h *History) SyncPodcasts(uris []spotify.URI) {
	present := make(map[spotify.URI]bool, len(uris))
	for _, uri := range uris {
		present[uri] = true
	}

	played := len(h.Podcasts.LastPlayed)
	maps.DeleteFunc(h.Podcasts.LastPlayed, func(uri spotify.URI, _ time.Time) bool {
		return !present[uri]
	})
	if len(h.Podcasts.LastPlayed) < played {
		h.revision++
	}
}

func (h History) PodcastLastPlayed(uri spotify.URI) time.Time {
	return h.Podcasts.LastPlayed[uri]
}

func (h History) Clone() History {
	h.Playlists = maps.Clone(h.Playlists)
	h.Podcasts.LastPlayed = maps.Clone(h.Podcasts.LastPlayed)
	return h
}

func (p Podcasts) IsZero() bool {
	return p.Order == "" && len(p.LastPlayed) == 0
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() (History, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return History{}, nil
	}
	if err != nil {
		return History{}, err
	}

	var h History
	if err := json.Unmarshal(b, &h); err != nil {
		return History{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return h, nil
}

func (s *Store) Save(h History) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if h.revision <= s.saved {
		return nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := writeFile(s.path, b); err != nil {
		return err
	}
	s.saved = h.revision
	return nil
}

func writeFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
