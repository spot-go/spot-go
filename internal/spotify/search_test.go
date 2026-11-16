package spotify

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestSearchReadsEveryType(t *testing.T) {
	var queries []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		queries = append(queries, q.Encode())
		if r.URL.Path != "/v1/search" || q.Get("limit") != "10" || q.Get("market") != "from_token" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		return jsonResponse(r, http.StatusOK, `{
			"tracks":{"total":2000,"items":[null,{"type":"track","name":"Come Together","uri":"spotify:track:t1","duration_ms":259000,"is_playable":false,"artists":[{"name":"The Beatles"}]}]},
			"artists":{"total":1,"items":[{"id":"ar1","uri":"spotify:artist:ar1","name":"The Beatles"}]},
			"albums":{"total":1,"items":[{"id":"a1","uri":"spotify:album:a1","name":"Abbey Road","album_type":"album","release_date":"1969-09-26","release_date_precision":"day","artists":[{"name":"The Beatles"}]}]},
			"playlists":{"total":3,"items":[null,{"id":"p1","uri":"spotify:playlist:p1","name":"Best","owner":{"display_name":"Ana"}},{"id":"p2","uri":"spotify:playlist:p2","name":"No owner","owner":null}]},
			"shows":{"total":1,"items":[{"id":"s1","uri":"spotify:show:s1","name":"Daily","publisher":"NYT"}]},
			"episodes":{"total":0,"items":[]}
		}`)
	})

	got, err := c.Search(context.Background(), "the beatles", 990)
	if err != nil {
		t.Fatal(err)
	}
	if want := "limit=10&market=from_token&offset=990&q=the+beatles&type=track%2Cartist%2Calbum%2Cplaylist%2Cshow%2Cepisode"; queries[0] != want {
		t.Fatalf("query = %s, want %s", queries[0], want)
	}
	wantSongs := []PlaylistItem{{Position: 991, Name: "Come Together", Artists: []string{"The Beatles"}, Duration: 259 * time.Second, URI: "spotify:track:t1"}}
	if !reflect.DeepEqual(got.Songs, wantSongs) {
		t.Fatalf("songs = %+v, want %+v", got.Songs, wantSongs)
	}
	if got.Artists[0].Name != "The Beatles" || got.Albums[0].Released.Year() != 1969 || got.Shows[0].Publisher != "NYT" {
		t.Fatalf("results = %+v", got)
	}
	wantPlaylists := []SearchPlaylist{{ID: "p1", URI: "spotify:playlist:p1", Name: "Best", Owner: "Ana"}, {ID: "p2", URI: "spotify:playlist:p2", Name: "No owner"}}
	if !reflect.DeepEqual(got.Playlists, wantPlaylists) {
		t.Fatalf("playlists = %+v, want %+v", got.Playlists, wantPlaylists)
	}
	wantPages := map[SearchType]SearchPage{
		SearchSongs:     {Next: 992, Total: 2000},
		SearchArtists:   {Next: 991, Total: 1, Last: true},
		SearchAlbums:    {Next: 991, Total: 1, Last: true},
		SearchPlaylists: {Next: 993, Total: 3, Last: true},
		SearchShows:     {Next: 991, Total: 1, Last: true},
		SearchEpisodes:  {Next: 990, Total: 0, Last: true},
	}
	if !reflect.DeepEqual(got.Pages, wantPages) {
		t.Fatalf("pages = %+v, want %+v", got.Pages, wantPages)
	}
}

func TestSearchStopsAtTheLastOffset(t *testing.T) {
	if page := searchPage(990, 5000, 10); !page.Last {
		t.Fatalf("page = %+v, want the last page at offset 1000", page)
	}
	if page := searchPage(980, 5000, 10); page.Last {
		t.Fatalf("page = %+v, want more pages", page)
	}
}

func TestSearchOnlyAsksForTheGivenTypes(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if got := r.URL.Query().Get("type"); got != "episode" {
			t.Fatalf("type = %q, want episode", got)
		}
		return jsonResponse(r, http.StatusOK, `{"episodes":{"total":1,"items":[{"id":"e1","uri":"spotify:episode:e1","name":"Ep","duration_ms":60000,"resume_point":{"fully_played":false,"resume_position_ms":30000}}]}}`)
	})
	got, err := c.Search(context.Background(), "x", 0, SearchEpisodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Episodes) != 1 || got.Episodes[0].Resume != 30*time.Second || len(got.Pages) != 1 {
		t.Fatalf("results = %+v", got)
	}
}
