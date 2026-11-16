package spotify

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestQueueReadsSongsAndEpisodes(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/me/player/queue" {
			return jsonResponse(r, http.StatusNotFound, `{"error":{"status":404,"message":"unexpected request"}}`)
		}
		return jsonResponse(r, http.StatusOK, `{
			"currently_playing":{"type":"track","name":"Come Together","uri":"spotify:track:t1","duration_ms":259000,"artists":[{"name":"The Beatles"}]},
			"queue":[
				{"type":"track","name":"Something","uri":"spotify:track:t2","duration_ms":182000,"artists":[{"name":"The Beatles"},{"name":"George Harrison"}]},
				null,
				{"type":"track","name":"No URI","uri":""},
				{"type":"episode","name":"How the Vote Count Works","uri":"spotify:episode:e1","duration_ms":3134000,"show":{"name":"Morning Briefing"}}
			]
		}`)
	})

	got, err := c.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Queue{
		Playing: &PlaylistItem{Name: "Come Together", Artists: []string{"The Beatles"}, Duration: 259 * time.Second, URI: "spotify:track:t1", Playable: true},
		Items: []PlaylistItem{
			{Position: 0, Name: "Something", Artists: []string{"The Beatles", "George Harrison"}, Duration: 182 * time.Second, URI: "spotify:track:t2", Playable: true},
			{Position: 3, Name: "How the Vote Count Works", Artists: []string{"Morning Briefing"}, Duration: 3134 * time.Second, URI: "spotify:episode:e1", Playable: true},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue = %+v, want %+v", got, want)
	}
}

func TestQueueWithNothingPlaying(t *testing.T) {
	for _, body := range []string{`{"currently_playing":null,"queue":[]}`, ""} {
		c := testClient(func(r *http.Request) *http.Response {
			if body == "" {
				return jsonResponse(r, http.StatusNoContent, "")
			}
			return jsonResponse(r, http.StatusOK, body)
		})

		got, err := c.Queue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.Playing != nil || len(got.Items) != 0 {
			t.Fatalf("queue for %q = %+v, want nothing", body, got)
		}
	}
}

func TestQueueError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`)
	})

	if _, err := c.Queue(context.Background()); err == nil || err.Error() != "The access token expired" {
		t.Fatalf("err = %v, want the API error", err)
	}
}
