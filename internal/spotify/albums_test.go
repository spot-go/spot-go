package spotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

func savedAlbumJSON(i int, added time.Time) string {
	return fmt.Sprintf(`{"added_at":%q,"album":{"id":"a%d","uri":"spotify:album:a%d","name":"Album %d","album_type":"album","release_date":"1997-05-21","release_date_precision":"day","total_tracks":12,"artists":[{"name":"Artist %d"},{"name":"Guest"}],"tracks":{"items":[],"total":12}}}`,
		added.Format(time.RFC3339), i, i, i, i)
}

func savedAlbumsPage(offset, total int, more bool, items []string) string {
	next := "null"
	if more {
		next = fmt.Sprintf(`"https://api.spotify.com/v1/me/albums?offset=%d&limit=50"`, offset+50)
	}
	return fmt.Sprintf(`{"items":[%s],"total":%d,"next":%s}`, strings.Join(items, ","), total, next)
}

func TestAllSavedAlbumsFollowsShortPages(t *testing.T) {
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	added := func(i int) time.Time { return base.Add(-time.Duration(i) * time.Hour) }
	var offsets []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		offsets = append(offsets, q.Get("offset"))
		if r.URL.Path != "/v1/me/albums" || q.Get("limit") != "50" || q.Get("market") != "from_token" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		var items []string
		switch q.Get("offset") {
		case "0":
			for i := range 50 {
				switch i {
				case 3:
					items = append(items, `{"added_at":"2026-11-07T10:00:00Z","album":null}`)
				case 10:
					items = append(items, `{"added_at":"2026-11-07T10:00:00Z","album":{"id":"","name":"No ID"}}`)
				case 20:
				default:
					items = append(items, savedAlbumJSON(i, added(i)))
				}
			}
			return jsonResponse(r, http.StatusOK, savedAlbumsPage(0, 200, true, items))
		case "50":
			items = append(items, savedAlbumJSON(49, added(49)))
			for i := 50; i < 60; i++ {
				items = append(items, savedAlbumJSON(i, added(i)))
			}
			return jsonResponse(r, http.StatusOK, savedAlbumsPage(50, 200, false, items))
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	albums, err := c.AllSavedAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(offsets, ",") != "0,50" {
		t.Fatalf("offsets = %v, want 0,50", offsets)
	}
	var want []int
	for i := range 60 {
		if i != 3 && i != 10 && i != 20 {
			want = append(want, i)
		}
	}
	if len(albums) != len(want) {
		t.Fatalf("albums = %d, want %d: a null album, an album without an ID or a repeated album was kept", len(albums), len(want))
	}
	for j, i := range want {
		got := albums[j]
		id := spotify.ID(fmt.Sprintf("a%d", i))
		if got.ID != id || got.URI != spotify.URI("spotify:album:"+id) || got.Name != fmt.Sprintf("Album %d", i) || !got.AddedAt.Equal(added(i)) {
			t.Fatalf("albums[%d] = %+v, want a%d in the API order", j, got, i)
		}
	}
	first := albums[0]
	if !reflect.DeepEqual(first.Artists, []string{"Artist 0", "Guest"}) || first.Type != "album" || first.Tracks != 12 ||
		first.Precision != "day" || !first.Released.Equal(time.Date(1997, 5, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("albums[0] = %+v, want its artists, type, track count and release date", first)
	}
}

func TestAllSavedAlbumsStopsOnAnEmptyPage(t *testing.T) {
	var offsets []string
	c := testClient(func(r *http.Request) *http.Response {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		var items []string
		switch offset {
		case "0":
			for i := range 50 {
				items = append(items, savedAlbumJSON(i, time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)))
			}
			return jsonResponse(r, http.StatusOK, savedAlbumsPage(0, 120, true, items))
		case "50":
			return jsonResponse(r, http.StatusOK, savedAlbumsPage(50, 120, true, nil))
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	albums, err := c.AllSavedAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(offsets, ",") != "0,50" {
		t.Fatalf("offsets = %v, want 0,50", offsets)
	}
	if len(albums) != 50 {
		t.Fatalf("albums = %d, want 50", len(albums))
	}
}

func TestAllSavedAlbumsError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if r.URL.Query().Get("offset") == "0" {
			return jsonResponse(r, http.StatusOK, savedAlbumsPage(0, 60, true, []string{savedAlbumJSON(0, time.Now())}))
		}
		return jsonResponse(r, http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`)
	})

	albums, err := c.AllSavedAlbums(context.Background())

	var apiErr spotify.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want the 401 from the API", err)
	}
	if albums != nil {
		t.Fatalf("albums = %+v, want nil on an error", albums)
	}
}

func TestAlbumTracksPage(t *testing.T) {
	var requests []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		requests = append(requests, q.Get("offset"))
		if r.URL.Path != "/v1/albums/a1/tracks" || q.Get("market") != "from_token" || q.Get("limit") != "50" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		switch q.Get("offset") {
		case "0":
			return jsonResponse(r, http.StatusOK, `{"total":52,"items":[
				{"type":"track","name":"Airbag","uri":"spotify:track:t1","duration_ms":284000,"track_number":1,"disc_number":1,"artists":[{"name":"Radiohead"}]},
				null,
				{"type":"track","name":"Paranoid Android","uri":"spotify:track:t3","duration_ms":383000,"is_playable":false,"artists":[{"name":"Radiohead"},{"name":"Guest"}]}
			]}`)
		case "50":
			return jsonResponse(r, http.StatusOK, `{"total":52,"items":[{"type":"track","name":"Fifty one","uri":"spotify:track:t51","is_playable":true},{"type":"track","name":"Fifty two","uri":"spotify:track:t52"}]}`)
		case "100":
			return jsonResponse(r, http.StatusOK, `{"total":200,"items":[]}`)
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	page, err := c.AlbumTracksPage(context.Background(), "a1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 3 || page.Total != 52 || page.Last {
		t.Fatalf("page = next %d, total %d, last %v, want 3, 52, false: Next must count the null items", page.Next, page.Total, page.Last)
	}
	want := []PlaylistItem{
		{Position: 0, Name: "Airbag", Artists: []string{"Radiohead"}, Duration: 284 * time.Second, URI: "spotify:track:t1", Playable: true},
		{Position: 2, Name: "Paranoid Android", Artists: []string{"Radiohead", "Guest"}, Duration: 383 * time.Second, URI: "spotify:track:t3"},
	}
	if !reflect.DeepEqual(page.Items, want) {
		t.Fatalf("items = %+v, want %+v", page.Items, want)
	}

	page, err = c.AlbumTracksPage(context.Background(), "a1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 52 || !page.Last || len(page.Items) != 2 || page.Items[1].Position != 51 || !page.Items[1].Playable {
		t.Fatalf("page = %+v, want the last page with tracks 51 and 52", page)
	}

	page, err = c.AlbumTracksPage(context.Background(), "a1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Last || len(page.Items) != 0 {
		t.Fatalf("page = %+v, want an empty page to be the last", page)
	}

	if strings.Join(requests, ",") != "0,50,100" {
		t.Fatalf("requests = %v, want 0,50,100", requests)
	}
}

func TestPlayContext(t *testing.T) {
	var path, body, deviceID string
	c := testClient(func(r *http.Request) *http.Response {
		b, _ := io.ReadAll(r.Body)
		path, body, deviceID = r.Method+" "+r.URL.Path, strings.TrimSpace(string(b)), r.URL.Query().Get("device_id")
		return jsonResponse(r, http.StatusNoContent, ``)
	})

	if err := c.PlayContext(context.Background(), "d1", "spotify:album:a1", 3); err != nil {
		t.Fatal(err)
	}
	if path != "PUT /v1/me/player/play" || deviceID != "d1" {
		t.Fatalf("request = %s on %q, want PUT /v1/me/player/play on d1", path, deviceID)
	}
	if want := `{"context_uri":"spotify:album:a1","offset":{"position":3}}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}

	if err := c.PlayArtist(context.Background(), "d1", "spotify:artist:ar1"); err != nil {
		t.Fatal(err)
	}
	if want := `{"context_uri":"spotify:artist:ar1"}`; body != want {
		t.Fatalf("body = %s, want %s: artist contexts take no offset", body, want)
	}
}

func TestRecentlyPlayedAlbumsAndArtists(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusOK, `{"items":[
			{"played_at":"2026-11-07T10:00:00Z","context":{"type":"playlist","uri":"spotify:playlist:a"}},
			{"played_at":"2026-11-07T09:00:00Z","context":{"type":"album","uri":"spotify:album:b"}},
			{"played_at":"2026-11-07T08:00:00Z","context":{"type":"artist","uri":"spotify:artist:c"}},
			{"played_at":"2026-11-07T07:00:00Z","context":null}
		]}`)
	})

	albums, err := c.RecentlyPlayedAlbums(context.Background())
	if err != nil || len(albums) != 1 || albums[0].URI != "spotify:album:b" {
		t.Fatalf("albums = %+v, %v, want only spotify:album:b", albums, err)
	}
	artists, err := c.RecentlyPlayedArtists(context.Background())
	if err != nil || len(artists) != 1 || artists[0].URI != "spotify:artist:c" {
		t.Fatalf("artists = %+v, %v, want only spotify:artist:c", artists, err)
	}
}
