package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/path"
	"github.com/zmb3/spotify/v2"
)

type (
	Order string

	Entry struct {
		AddedAt    time.Time `json:"addedAt,omitzero"`
		LastPlayed time.Time `json:"lastPlayed,omitzero"`
	}

	History struct {
		Order     Order                 `json:"order,omitempty"`
		Synced    bool                  `json:"synced,omitempty"`
		Playlists map[spotify.URI]Entry `json:"playlists,omitempty"`
		Podcasts  Saved                 `json:"podcasts,omitzero"`
		Albums    Saved                 `json:"albums,omitzero"`
		Artists   Artists               `json:"artists,omitzero"`
		revision  int
	}

	Saved struct {
		Order      Order                     `json:"order,omitempty"`
		LastPlayed map[spotify.URI]time.Time `json:"lastPlayed,omitempty"`
	}

	Artists struct {
		Order    Order                 `json:"order,omitempty"`
		Synced   bool                  `json:"synced,omitempty"`
		Followed map[spotify.URI]Entry `json:"followed,omitempty"`
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
	h.setOrder(&h.Order, o)
}

func (h *History) Sync(uris []spotify.URI, now time.Time) {
	h.syncEntries(&h.Playlists, &h.Synced, uris, now)
}

func (h *History) Played(uri spotify.URI, at time.Time) bool {
	return h.playEntry(h.Playlists, uri, at)
}

func (h History) Recent(uri spotify.URI) time.Time {
	return h.Playlists[uri].recent()
}

func (h *History) SetPodcastOrder(o Order) {
	h.setOrder(&h.Podcasts.Order, o)
}

func (h *History) PodcastPlayed(uri spotify.URI, at time.Time) bool {
	return h.playSaved(&h.Podcasts, uri, at)
}

func (h *History) SyncPodcasts(uris []spotify.URI) {
	h.syncSaved(&h.Podcasts, uris)
}

func (h History) PodcastLastPlayed(uri spotify.URI) time.Time {
	return h.Podcasts.LastPlayed[uri]
}

func (h *History) SetAlbumOrder(o Order) {
	h.setOrder(&h.Albums.Order, o)
}

func (h *History) AlbumPlayed(uri spotify.URI, at time.Time) bool {
	return h.playSaved(&h.Albums, uri, at)
}

func (h *History) SyncAlbums(uris []spotify.URI) {
	h.syncSaved(&h.Albums, uris)
}

func (h History) AlbumLastPlayed(uri spotify.URI) time.Time {
	return h.Albums.LastPlayed[uri]
}

func (h *History) SetArtistOrder(o Order) {
	h.setOrder(&h.Artists.Order, o)
}

func (h *History) SyncArtists(uris []spotify.URI, now time.Time) {
	h.syncEntries(&h.Artists.Followed, &h.Artists.Synced, uris, now)
}

func (h *History) ArtistPlayed(uri spotify.URI, at time.Time) bool {
	return h.playEntry(h.Artists.Followed, uri, at)
}

func (h History) ArtistRecent(uri spotify.URI) time.Time {
	return h.Artists.Followed[uri].recent()
}

func (h History) ArtistAdded(uri spotify.URI) time.Time {
	return h.Artists.Followed[uri].AddedAt
}

func (h History) Clone() History {
	h.Playlists = maps.Clone(h.Playlists)
	h.Podcasts.LastPlayed = maps.Clone(h.Podcasts.LastPlayed)
	h.Albums.LastPlayed = maps.Clone(h.Albums.LastPlayed)
	h.Artists.Followed = maps.Clone(h.Artists.Followed)
	return h
}

func (s Saved) IsZero() bool {
	return s.Order == "" && len(s.LastPlayed) == 0
}

func (a Artists) IsZero() bool {
	return a.Order == "" && !a.Synced && len(a.Followed) == 0
}

func (e Entry) recent() time.Time {
	if e.LastPlayed.After(e.AddedAt) {
		return e.LastPlayed
	}
	return e.AddedAt
}

func (h *History) setOrder(current *Order, o Order) {
	if *current == o {
		return
	}
	*current = o
	h.revision++
}

func (h *History) syncEntries(entries *map[spotify.URI]Entry, synced *bool, uris []spotify.URI, now time.Time) {
	changed := !*synced
	if *entries == nil {
		*entries = make(map[spotify.URI]Entry, len(uris))
	}

	present := make(map[spotify.URI]bool, len(uris))
	for _, uri := range uris {
		present[uri] = true
		if _, ok := (*entries)[uri]; ok {
			continue
		}
		var e Entry
		if *synced {
			e.AddedAt = now
		}
		(*entries)[uri] = e
		changed = true
	}
	for uri := range *entries {
		if !present[uri] {
			delete(*entries, uri)
			changed = true
		}
	}

	*synced = true
	if changed {
		h.revision++
	}
}

func (h *History) playEntry(entries map[spotify.URI]Entry, uri spotify.URI, at time.Time) bool {
	e, ok := entries[uri]
	if !ok || !at.After(e.LastPlayed) {
		return false
	}
	e.LastPlayed = at
	entries[uri] = e
	h.revision++
	return true
}

func (h *History) playSaved(s *Saved, uri spotify.URI, at time.Time) bool {
	if !at.After(s.LastPlayed[uri]) {
		return false
	}
	if s.LastPlayed == nil {
		s.LastPlayed = make(map[spotify.URI]time.Time)
	}
	s.LastPlayed[uri] = at
	h.revision++
	return true
}

func (h *History) syncSaved(s *Saved, uris []spotify.URI) {
	present := make(map[spotify.URI]bool, len(uris))
	for _, uri := range uris {
		present[uri] = true
	}

	played := len(s.LastPlayed)
	maps.DeleteFunc(s.LastPlayed, func(uri spotify.URI, _ time.Time) bool {
		return !present[uri]
	})
	if len(s.LastPlayed) < played {
		h.revision++
	}
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
	if err := path.WriteFile(s.path, b); err != nil {
		return err
	}
	s.saved = h.revision
	return nil
}
