package spotify

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
)

func TestLikedSongsPage(t *testing.T) {
	var requests []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		requests = append(requests, q.Get("offset"))
		if r.URL.Path != "/v1/me/tracks" || q.Get("market") != "from_token" || q.Get("limit") != "50" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		switch q.Get("offset") {
		case "0":
			return jsonResponse(r, http.StatusOK, `{"total":52,"items":[
				{"added_at":"2026-11-01T10:00:00Z","track":{"type":"track","name":"Baby","uri":"spotify:track:t1","duration_ms":211000,"is_local":false,"is_playable":true,"artists":[{"name":"Gal Costa"},{"name":"Caetano Veloso"}]}},
				{"added_at":"2026-10-01T10:00:00Z","track":null},
				{"added_at":"2026-09-01T10:00:00Z","track":{"type":"track","name":"Aquarela","uri":"spotify:track:t3","duration_ms":279000,"is_playable":true,"artists":[{"name":"Toquinho"}]}},
				{"added_at":"2026-08-01T10:00:00Z","track":{"type":"track","name":"Gone","uri":"spotify:track:gone","duration_ms":1000,"is_playable":false,"artists":[]}},
				{"added_at":"2026-07-01T10:00:00Z","track":{"type":"track","name":"Demo","uri":"spotify:local:::Demo:100","duration_ms":100000,"is_local":true,"artists":[]}}
			]}`)
		case "50":
			return jsonResponse(r, http.StatusOK, `{"total":52,"items":[{"track":{"type":"track","name":"Fifty one","uri":"spotify:track:t51"}},{"track":{"type":"track","name":"Fifty two","uri":"spotify:track:t52"}}]}`)
		case "100":
			return jsonResponse(r, http.StatusOK, `{"total":200,"items":[]}`)
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	page, err := c.LikedSongsPage(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 5 || page.Total != 52 || page.Last {
		t.Fatalf("page = next %d, total %d, last %v, want 5, 52, false: Next must count the null tracks", page.Next, page.Total, page.Last)
	}
	want := []PlaylistItem{
		{Position: 0, Name: "Baby", Artists: []string{"Gal Costa", "Caetano Veloso"}, Duration: 211 * time.Second, URI: "spotify:track:t1", Playable: true},
		{Position: 2, Name: "Aquarela", Artists: []string{"Toquinho"}, Duration: 279 * time.Second, URI: "spotify:track:t3", Playable: true},
		{Position: 3, Name: "Gone", Artists: []string{}, Duration: time.Second, URI: "spotify:track:gone"},
		{Position: 4, Name: "Demo", Artists: []string{}, Duration: 100 * time.Second, URI: "spotify:local:::Demo:100", IsLocal: true},
	}
	if !reflect.DeepEqual(page.Items, want) {
		t.Fatalf("items = %+v, want %+v", page.Items, want)
	}

	page, err = c.LikedSongsPage(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 52 || !page.Last || len(page.Items) != 2 || page.Items[1].Position != 51 || !page.Items[1].Playable {
		t.Fatalf("page = %+v, want the last page with songs 51 and 52", page)
	}

	page, err = c.LikedSongsPage(context.Background(), 100)
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

func TestLikedSongsPageError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
	})

	if _, err := c.LikedSongsPage(context.Background(), 0); err == nil || err.Error() != "Insufficient client scope" {
		t.Fatalf("error = %v, want Insufficient client scope", err)
	}
}

func TestPlayContextFromAndPlaySongs(t *testing.T) {
	var path, body, deviceID string
	c := testClient(func(r *http.Request) *http.Response {
		b, _ := io.ReadAll(r.Body)
		path, body, deviceID = r.Method+" "+r.URL.Path, strings.TrimSpace(string(b)), r.URL.Query().Get("device_id")
		return jsonResponse(r, http.StatusNoContent, ``)
	})

	if err := c.PlayContextFrom(context.Background(), "d1", "spotify:user:u:collection", "spotify:track:t1"); err != nil {
		t.Fatal(err)
	}
	if path != "PUT /v1/me/player/play" || deviceID != "d1" {
		t.Fatalf("request = %s on %q, want PUT /v1/me/player/play on d1", path, deviceID)
	}
	if want := `{"context_uri":"spotify:user:u:collection","offset":{"uri":"spotify:track:t1"}}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}

	if err := c.PlayContextFrom(context.Background(), "", "spotify:user:u:collection", ""); err != nil {
		t.Fatal(err)
	}
	if want := `{"context_uri":"spotify:user:u:collection"}`; body != want || deviceID != "" {
		t.Fatalf("body = %s on %q, want %s on the active device: no song plays from the start", body, deviceID, want)
	}

	if err := c.PlaySongs(context.Background(), "d1", []spotify.URI{"spotify:track:t1", "spotify:track:t2"}); err != nil {
		t.Fatal(err)
	}
	if want := `{"uris":["spotify:track:t1","spotify:track:t2"],"offset":{"position":0}}`; body != want || deviceID != "d1" {
		t.Fatalf("body = %s on %q, want %s on d1: the first song must be where it starts", body, deviceID, want)
	}
}
