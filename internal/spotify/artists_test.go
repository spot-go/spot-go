package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

func followedArtistsPage(total int, after string, items []string) string {
	next, cursor := "null", "null"
	if after != "" {
		next = fmt.Sprintf(`"https://api.spotify.com/v1/me/following?type=artist&after=%s&limit=50"`, after)
		cursor = fmt.Sprintf("%q", after)
	}
	return fmt.Sprintf(`{"artists":{"items":[%s],"total":%d,"next":%s,"cursors":{"after":%s}}}`, strings.Join(items, ","), total, next, cursor)
}

func artistJSON(i int) string {
	return fmt.Sprintf(`{"id":"ar%d","uri":"spotify:artist:ar%d","name":"Artist %d"}`, i, i, i)
}

func TestAllFollowedArtistsFollowsTheCursor(t *testing.T) {
	var cursors []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		cursors = append(cursors, q.Get("after"))
		if r.URL.Path != "/v1/me/following" || q.Get("type") != "artist" || q.Get("limit") != "50" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		var items []string
		switch q.Get("after") {
		case "":
			if q.Has("after") {
				return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"empty after"}}`)
			}
			for i := range 50 {
				switch i {
				case 3:
					items = append(items, `null`)
				case 10:
					items = append(items, `{"id":"","name":"No ID"}`)
				default:
					items = append(items, artistJSON(i))
				}
			}
			return jsonResponse(r, http.StatusOK, followedArtistsPage(55, "ar49", items))
		case "ar49":
			items = append(items, artistJSON(49))
			for i := 50; i < 55; i++ {
				items = append(items, artistJSON(i))
			}
			return jsonResponse(r, http.StatusOK, followedArtistsPage(55, "", items))
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	artists, err := c.AllFollowedArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(cursors, ",") != ",ar49" {
		t.Fatalf("cursors = %v, want the first page and then after=ar49", cursors)
	}
	if len(artists) != 53 {
		t.Fatalf("artists = %d, want 53: a null artist, an artist without an ID or a repeated artist was kept", len(artists))
	}
	if want := (Artist{ID: "ar0", URI: "spotify:artist:ar0", Name: "Artist 0"}); artists[0] != want {
		t.Fatalf("artists[0] = %+v, want %+v", artists[0], want)
	}
	if artists[len(artists)-1].ID != "ar54" {
		t.Fatalf("last artist = %s, want ar54", artists[len(artists)-1].ID)
	}
}

func TestAllFollowedArtistsStopsWhenTheCursorDoesNotMove(t *testing.T) {
	requests := 0
	c := testClient(func(r *http.Request) *http.Response {
		requests++
		if requests > 3 {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"too many requests"}}`)
		}
		return jsonResponse(r, http.StatusOK, followedArtistsPage(100, "ar0", []string{artistJSON(0)}))
	})

	artists, err := c.AllFollowedArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(artists) != 1 {
		t.Fatalf("requests = %d, artists = %d, want 2 requests and 1 artist", requests, len(artists))
	}
}

func TestAllFollowedArtistsError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
	})

	artists, err := c.AllFollowedArtists(context.Background())

	var apiErr spotify.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("err = %v, want the 403 from the API", err)
	}
	if artists != nil {
		t.Fatalf("artists = %+v, want nil on an error", artists)
	}
}

func TestArtistAlbumsPage(t *testing.T) {
	var requests []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		requests = append(requests, q.Get("offset"))
		if r.URL.Path != "/v1/artists/ar1/albums" || q.Get("market") != "from_token" || q.Get("limit") != "10" || q.Get("include_groups") != "album,single,compilation" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		switch q.Get("offset") {
		case "0":
			return jsonResponse(r, http.StatusOK, `{"total":12,"items":[
				{"id":"a1","uri":"spotify:album:a1","name":"OK Computer","album_type":"album","release_date":"1997-05-21","release_date_precision":"day","total_tracks":12,"artists":[{"name":"Radiohead"}]},
				null,
				{"id":"a2","uri":"spotify:album:a2","name":"Creep","album_type":"single","release_date":"1992","total_tracks":2}
			]}`)
		case "10":
			return jsonResponse(r, http.StatusOK, `{"total":12,"items":[{"id":"a11","name":"Eleven"},{"id":"a12","name":"Twelve"}]}`)
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	page, err := c.ArtistAlbumsPage(context.Background(), "ar1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 3 || page.Total != 12 || page.Last {
		t.Fatalf("page = next %d, total %d, last %v, want 3, 12, false: Next must count the null items", page.Next, page.Total, page.Last)
	}
	want := []Album{
		{ID: "a1", URI: "spotify:album:a1", Name: "OK Computer", Artists: []string{"Radiohead"}, Type: "album", Released: time.Date(1997, 5, 21, 0, 0, 0, 0, time.UTC)},
		{ID: "a2", URI: "spotify:album:a2", Name: "Creep", Artists: []string{}, Type: "single", Released: time.Date(1992, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	if !reflect.DeepEqual(page.Items, want) {
		t.Fatalf("items = %+v, want %+v", page.Items, want)
	}

	page, err = c.ArtistAlbumsPage(context.Background(), "ar1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 12 || !page.Last || len(page.Items) != 2 {
		t.Fatalf("page = %+v, want the last page", page)
	}
	if strings.Join(requests, ",") != "0,10" {
		t.Fatalf("requests = %v, want 0,10", requests)
	}
}
