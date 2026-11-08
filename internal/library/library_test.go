package library

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

var (
	day1 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	day2 = day1.Add(24 * time.Hour)
	day3 = day2.Add(24 * time.Hour)
)

func saveWrites(t *testing.T, store *Store, h History) bool {
	t.Helper()
	if err := store.Save(h.Clone()); err != nil {
		t.Fatal(err)
	}
	err := os.Remove(store.path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func TestFirstSyncDoesNotMarkPlaylistsAsAdded(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a", "b"}, day1)

	if !h.Synced {
		t.Fatal("history is not synced")
	}
	for _, uri := range []spotify.URI{"a", "b"} {
		if got := h.Playlists[uri]; !got.AddedAt.IsZero() {
			t.Fatalf("%s added at %v on the first sync, want unknown", uri, got.AddedAt)
		}
	}
}

func TestLaterSyncsMarkNewPlaylistsAsAdded(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a"}, day1)
	h.Sync([]spotify.URI{"a", "b"}, day2)

	if got := h.Playlists["b"].AddedAt; !got.Equal(day2) {
		t.Fatalf("b added at %v, want %v", got, day2)
	}
	if got := h.Playlists["a"].AddedAt; !got.IsZero() {
		t.Fatalf("a added at %v, want unknown", got)
	}
}

func TestSyncForgetsRemovedPlaylists(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a", "b"}, day1)
	h.Played("b", day1)
	h.Sync([]spotify.URI{"a"}, day2)

	if _, ok := h.Playlists["b"]; ok {
		t.Fatal("b is still in the history after it left the library")
	}

	h.Sync([]spotify.URI{"a", "b"}, day3)
	if got := h.Playlists["b"]; !got.AddedAt.Equal(day3) || !got.LastPlayed.IsZero() {
		t.Fatalf("b after it came back = %+v, want added at %v and never played", got, day3)
	}
}

func TestPlayedKeepsTheLatestTime(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a"}, day1)

	if !h.Played("a", day2) {
		t.Fatal("first play was not recorded")
	}
	if h.Played("a", day1) {
		t.Fatal("an older play replaced a newer one")
	}
	if h.Played("unknown", day3) {
		t.Fatal("a playlist outside the library was recorded")
	}
	if got := h.Playlists["a"].LastPlayed; !got.Equal(day2) {
		t.Fatalf("last played = %v, want %v", got, day2)
	}
}

func TestRecentIsTheLatestOfAddedAndPlayed(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a"}, day1)
	h.Sync([]spotify.URI{"a", "b"}, day2)
	h.Played("a", day3)
	h.Played("b", day1)

	if got := h.Recent("a"); !got.Equal(day3) {
		t.Fatalf("recent a = %v, want %v", got, day3)
	}
	if got := h.Recent("b"); !got.Equal(day2) {
		t.Fatalf("recent b = %v, want %v", got, day2)
	}
	if got := h.Recent("unknown"); !got.IsZero() {
		t.Fatalf("recent unknown = %v, want zero", got)
	}
}

func TestCloneDoesNotShareThePlaylists(t *testing.T) {
	var h History
	h.Sync([]spotify.URI{"a"}, day1)
	c := h.Clone()
	h.Played("a", day2)

	if got := c.Playlists["a"].LastPlayed; !got.IsZero() {
		t.Fatalf("clone changed with the original: last played = %v", got)
	}
}

func TestPodcastPlayedKeepsTheLatest(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "library.json"))
	var h History

	if !h.PodcastPlayed("a", day2) {
		t.Fatal("first play was not recorded")
	}
	if !saveWrites(t, store, h) {
		t.Fatal("first play was not saved")
	}
	if h.PodcastPlayed("a", day1) {
		t.Fatal("an older play replaced a newer one")
	}
	if h.PodcastPlayed("a", day2) {
		t.Fatal("the same play was recorded twice")
	}
	if saveWrites(t, store, h) {
		t.Fatal("plays that were not recorded were saved")
	}
	if got := h.PodcastLastPlayed("a"); !got.Equal(day2) {
		t.Fatalf("last played = %v, want %v", got, day2)
	}
	if got := h.PodcastLastPlayed("unknown"); !got.IsZero() {
		t.Fatalf("last played unknown = %v, want zero", got)
	}
	if h.Synced || h.Playlists != nil {
		t.Fatalf("a podcast play changed the playlists: %+v", h)
	}
}

func TestSyncPodcastsDropsUnfollowedShows(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "library.json"))
	var h History
	h.PodcastPlayed("a", day1)
	h.PodcastPlayed("b", day2)
	if !saveWrites(t, store, h) {
		t.Fatal("plays were not saved")
	}

	h.SyncPodcasts([]spotify.URI{"a", "b", "c"})
	if _, ok := h.Podcasts.LastPlayed["c"]; ok {
		t.Fatal("sync added a show that was never played")
	}
	if saveWrites(t, store, h) {
		t.Fatal("a sync that dropped nothing was saved")
	}

	h.SyncPodcasts([]spotify.URI{"a"})
	if _, ok := h.Podcasts.LastPlayed["b"]; ok {
		t.Fatal("b is still in the history after it was unfollowed")
	}
	if got := h.PodcastLastPlayed("a"); !got.Equal(day1) {
		t.Fatalf("last played a = %v, want %v", got, day1)
	}
	if !saveWrites(t, store, h) {
		t.Fatal("a sync that dropped b was not saved")
	}
	if h.Synced || h.Playlists != nil {
		t.Fatalf("a podcast sync changed the playlists: %+v", h)
	}
}

func TestSetPodcastOrderBumpsTheRevision(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "library.json"))
	var h History

	h.SetPodcastOrder(OrderRecentlyAdded)
	if h.Podcasts.Order != OrderRecentlyAdded || h.Order != "" {
		t.Fatalf("podcast order = %q and playlist order = %q, want %q and none", h.Podcasts.Order, h.Order, OrderRecentlyAdded)
	}
	if !saveWrites(t, store, h) {
		t.Fatal("a new podcast order was not saved")
	}

	h.SetPodcastOrder(OrderRecentlyAdded)
	if saveWrites(t, store, h) {
		t.Fatal("the same podcast order was saved again")
	}
}

func TestCloneCopiesThePodcastHistory(t *testing.T) {
	var h History
	h.PodcastPlayed("a", day1)
	c := h.Clone()
	h.PodcastPlayed("a", day2)
	h.PodcastPlayed("b", day2)

	if got := c.PodcastLastPlayed("a"); !got.Equal(day1) {
		t.Fatalf("clone changed with the original: last played a = %v, want %v", got, day1)
	}
	if _, ok := c.Podcasts.LastPlayed["b"]; ok {
		t.Fatal("clone changed with the original: b was played")
	}
}

func TestSyncLeavesThePodcastsAlone(t *testing.T) {
	var h History
	h.SetPodcastOrder(OrderRecentlyAdded)
	h.PodcastPlayed("show", day1)
	h.Sync([]spotify.URI{"a"}, day2)
	h.Sync(nil, day3)

	want := Podcasts{Order: OrderRecentlyAdded, LastPlayed: map[spotify.URI]time.Time{"show": day1}}
	if !reflect.DeepEqual(h.Podcasts, want) {
		t.Fatalf("podcasts after a playlist sync = %+v, want %+v", h.Podcasts, want)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	store := NewStore(path)

	h, err := store.Load()
	if err != nil || h.Synced || h.Playlists != nil || !h.Podcasts.IsZero() {
		t.Fatalf("load without a file = %+v, %v, want an empty history", h, err)
	}

	h.SetOrder(OrderCreator)
	h.Sync([]spotify.URI{"a"}, day1)
	h.Sync([]spotify.URI{"a", "b"}, day2)
	h.Played("a", day3)
	h.SetPodcastOrder(OrderRecentlyAdded)
	h.PodcastPlayed("show", day2)
	if err := store.Save(h.Clone()); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	loaded.revision = h.revision
	if !reflect.DeepEqual(loaded, h) {
		t.Fatalf("loaded = %+v, want %+v", loaded, h)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("config dir has %v, %v, want only library.json", entries, err)
	}
}

func TestStoreSkipsOlderHistories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	store := NewStore(path)

	var h History
	h.SetOrder(OrderCreator)
	older := h.Clone()
	h.SetOrder(OrderAlphabetical)

	if err := store.Save(h.Clone()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(older); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Order != OrderAlphabetical {
		t.Fatalf("order = %q, an older history overwrote the newer one", loaded.Order)
	}
}

func TestOldLibraryFileLoadsWithoutPodcasts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	old := `{"order":"creator","synced":true,"playlists":{"a":{"lastPlayed":"2026-11-01T10:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	h, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.Podcasts, Podcasts{}) {
		t.Fatalf("podcasts of an old file = %+v, want none", h.Podcasts)
	}
	if h.Order != OrderCreator || !h.Playlists["a"].LastPlayed.Equal(day1) {
		t.Fatalf("old file loaded as %+v", h)
	}

	h.SetOrder(OrderAlphabetical)
	h.PodcastPlayed("show", day2)
	h.SyncPodcasts(nil)
	if err := store.Save(h.Clone()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"podcasts"`)) {
		t.Fatalf("saved %s, want no podcasts section", b)
	}
}

func TestStoreReportsBrokenFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("load of a broken file did not fail")
	}
}

func TestStoreReportsWriteErrors(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing", "library.json"))

	var h History
	h.SetOrder(OrderCreator)
	if err := store.Save(h); err == nil {
		t.Fatal("save into a missing folder did not fail")
	}
}

func TestLaterSyncsAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	var h History
	h.Sync([]spotify.URI{"a", "b"}, day1)
	if err := NewStore(path).Save(h.Clone()); err != nil {
		t.Fatal(err)
	}

	for _, uris := range [][]spotify.URI{{"a", "b", "c"}, {"a", "c"}} {
		store := NewStore(path)
		loaded, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		loaded.Sync(uris, day2)
		if err := store.Save(loaded.Clone()); err != nil {
			t.Fatal(err)
		}

		saved, err := NewStore(path).Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Playlists) != len(uris) {
			t.Fatalf("saved playlists = %v, want %v", saved.Playlists, uris)
		}
		if got := saved.Playlists["c"].AddedAt; !got.Equal(day2) {
			t.Fatalf("c added at %v, want %v", got, day2)
		}
	}
}

func TestBrokenFilesAreReportedAsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(path, []byte(`{"synced":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(path).Load(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("load = %v, want ErrCorrupt", err)
	}
	if _, err := NewStore(t.TempDir()).Load(); err == nil || errors.Is(err, ErrCorrupt) {
		t.Fatalf("reading a folder = %v, want a read error that is not ErrCorrupt", err)
	}
}
