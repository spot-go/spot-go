package spotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

type roundTripper func(*http.Request) *http.Response

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

func jsonResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func testClient(fn func(*http.Request) *http.Response) *Client {
	c := NewClient(&http.Client{Transport: roundTripper(fn)})
	c.baseURL = "https://api.test/v1/"
	return c
}

func TestAllPlaylistsFollowsShortPages(t *testing.T) {
	const total = 60
	var offsets []string
	page := func(offset int) string {
		var items []string
		for i := offset; i < min(offset+50, total); i++ {
			if i == 3 || i == 10 || i == 20 {
				continue
			}
			items = append(items, fmt.Sprintf(`{"id":"p%d","name":"Playlist %d"}`, i, i))
		}
		if offset == 50 {
			items = append([]string{`{"id":"p49","name":"Playlist 49"}`}, items...)
		}
		next := "null"
		if offset+50 < total {
			next = `"https://api.spotify.com/v1/me/playlists?offset=50"`
		}
		return fmt.Sprintf(`{"items":[%s],"total":%d,"next":%s}`, strings.Join(items, ","), total, next)
	}
	c := NewClient(&http.Client{Transport: roundTripper(func(r *http.Request) *http.Response {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		n, _ := strconv.Atoi(offset)
		return jsonResponse(r, http.StatusOK, page(n))
	})})

	playlists, err := c.AllCurrentUsersPlaylists(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(playlists) != total-3 {
		t.Fatalf("playlists = %d, want %d: the pages were not followed to the end or a playlist repeated", len(playlists), total-3)
	}
	if strings.Join(offsets, ",") != "0,50" {
		t.Fatalf("offsets = %v, want 0,50", offsets)
	}
	if playlists[len(playlists)-1].ID != "p59" {
		t.Fatalf("last playlist = %s, want p59", playlists[len(playlists)-1].ID)
	}
}

func TestRecentlyPlayedPlaylists(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if r.URL.Path != "/v1/me/player/recently-played" || r.URL.Query().Get("limit") != "50" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		return jsonResponse(r, http.StatusOK, `{"items":[
			{"played_at":"2026-11-07T10:00:00.123Z","context":{"type":"playlist","uri":"spotify:playlist:a"}},
			{"played_at":"2026-11-07T09:00:00Z","context":{"type":"album","uri":"spotify:album:b"}},
			{"played_at":"2026-11-07T08:00:00Z","context":null},
			{"played_at":"2026-11-07T07:00:00Z"},
			{"played_at":"2026-11-07T06:00:00Z","context":{"type":"playlist","uri":"spotify:playlist:c"}}
		]}`)
	})

	played, err := c.RecentlyPlayedPlaylists(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []PlayedContext{
		{URI: "spotify:playlist:a", At: time.Date(2026, 11, 7, 10, 0, 0, 123000000, time.UTC)},
		{URI: "spotify:playlist:c", At: time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC)},
	}
	if len(played) != len(want) {
		t.Fatalf("played = %+v, want %+v", played, want)
	}
	for i := range want {
		if played[i].URI != want[i].URI || !played[i].At.Equal(want[i].At) {
			t.Fatalf("played = %+v, want %+v", played, want)
		}
	}
}

func TestNowPlayingContext(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   spotify.URI
		err    bool
	}{
		{"playlist", http.StatusOK, `{"is_playing":true,"context":{"type":"playlist","uri":"spotify:playlist:a"}}`, "spotify:playlist:a", false},
		{"paused", http.StatusOK, `{"is_playing":false,"context":{"type":"playlist","uri":"spotify:playlist:a"}}`, "", false},
		{"album", http.StatusOK, `{"is_playing":true,"context":{"type":"album","uri":"spotify:album:a"},"item":{"type":"track","name":"Song"}}`, "spotify:album:a", false},
		{"artist", http.StatusOK, `{"is_playing":true,"context":{"type":"artist","uri":"spotify:artist:a"},"item":{"type":"track","name":"Song"}}`, "spotify:artist:a", false},
		{"liked songs", http.StatusOK, `{"is_playing":true,"context":{"type":"collection","uri":"spotify:user:u:collection"},"item":{"type":"track","name":"Song"}}`, "", false},
		{"no context", http.StatusOK, `{"is_playing":true,"context":null}`, "", false},
		{"nothing plays", http.StatusNoContent, ``, "", false},
		{"error", http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`, "", true},
		{"show", http.StatusOK, `{"is_playing":true,"context":{"type":"show","uri":"spotify:show:s"},"item":{"type":"episode","show":{"uri":"spotify:show:other"}}}`, "spotify:show:s", false},
		{"episode without context", http.StatusOK, `{"is_playing":true,"context":null,"item":{"type":"episode","show":{"uri":"spotify:show:s"}}}`, "spotify:show:s", false},
		{"episode in a playlist", http.StatusOK, `{"is_playing":true,"context":{"type":"playlist","uri":"spotify:playlist:a"},"item":{"type":"episode","show":{"uri":"spotify:show:s"}}}`, "spotify:playlist:a", false},
		{"episode in your episodes", http.StatusOK, `{"is_playing":true,"context":{"type":"collection","uri":"spotify:user:u:collection:your-episodes"},"item":{"type":"episode","show":{"uri":"spotify:show:s"}}}`, "spotify:show:s", false},
		{"paused episode", http.StatusOK, `{"is_playing":false,"context":{"type":"show","uri":"spotify:show:s"},"item":{"type":"episode","show":{"uri":"spotify:show:s"}}}`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(func(r *http.Request) *http.Response {
				if r.URL.Path != "/v1/me/player" || r.URL.Query().Get("additional_types") != "episode" {
					return jsonResponse(r, http.StatusNotFound, `{"error":{"status":404,"message":"not found"}}`)
				}
				return jsonResponse(r, tt.status, tt.body)
			})

			got, err := c.NowPlaying(context.Background())

			if got.Context != tt.want || (err != nil) != tt.err {
				t.Fatalf("NowPlaying context = %q, %v, want %q, error %v", got.Context, err, tt.want, tt.err)
			}
		})
	}
}

func TestPlayback(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   Playback
		err    bool
	}{
		{"playing", http.StatusOK, `{"is_playing":true,"progress_ms":60000,"device":{"id":"phone"},"item":{"type":"track","uri":"spotify:track:t","name":"Paranoid","duration_ms":168000,"artists":[{"name":"Black Sabbath"},{"name":"Ozzy"}]}}`,
			Playback{Playing: true, Device: "phone", Item: "spotify:track:t", Name: "Paranoid", Artist: "Black Sabbath, Ozzy", Progress: time.Minute, Duration: 168 * time.Second}, false},
		{"paused episode", http.StatusOK, `{"is_playing":false,"device":{"id":"phone"},"context":null,"item":{"type":"episode","uri":"spotify:episode:e","name":"E","show":{"uri":"spotify:show:s","name":"The Daily"}}}`,
			Playback{Device: "phone", Item: "spotify:episode:e", Name: "E", Artist: "The Daily"}, false},
		{"no item", http.StatusOK, `{"is_playing":false,"device":{"id":"phone"},"item":null}`, Playback{Device: "phone"}, false},
		{"volume", http.StatusOK, `{"is_playing":true,"device":{"id":"phone","volume_percent":55,"supports_volume":true}}`,
			Playback{Playing: true, Device: "phone", Volume: 55, SupportsVolume: true}, false},
		{"volume without supports_volume", http.StatusOK, `{"is_playing":true,"device":{"id":"phone","volume_percent":0}}`,
			Playback{Playing: true, Device: "phone", SupportsVolume: true}, false},
		{"volume not supported", http.StatusOK, `{"is_playing":true,"device":{"id":"tv","volume_percent":40,"supports_volume":false}}`,
			Playback{Playing: true, Device: "tv"}, false},
		{"unknown volume", http.StatusOK, `{"is_playing":true,"device":{"id":"tv","volume_percent":null,"supports_volume":true}}`,
			Playback{Playing: true, Device: "tv"}, false},
		{"no device", http.StatusOK, `{"is_playing":false,"device":null}`, Playback{}, false},
		{"nothing plays", http.StatusNoContent, ``, Playback{}, false},
		{"error", http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`, Playback{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(func(r *http.Request) *http.Response {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/me/player" || r.URL.Query().Get("additional_types") != "episode" {
					return jsonResponse(r, http.StatusNotFound, `{"error":{"status":404,"message":"not found"}}`)
				}
				return jsonResponse(r, tt.status, tt.body)
			})

			got, err := c.Playback(context.Background())

			if got != tt.want || (err != nil) != tt.err {
				t.Fatalf("Playback = %+v, %v, want %+v, error %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestResumePlayback(t *testing.T) {
	type request struct {
		path, device, body string
		hasDevice          bool
	}
	var got request
	c := testClient(func(r *http.Request) *http.Response {
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		got = request{
			path:      r.Method + " " + r.URL.Path,
			device:    r.URL.Query().Get("device_id"),
			hasDevice: r.URL.Query().Has("device_id"),
			body:      strings.TrimSpace(string(b)),
		}
		return jsonResponse(r, http.StatusNoContent, ``)
	})

	for _, tt := range []struct {
		name string
		call func() error
		want request
	}{
		{"resume", func() error { return c.ResumePlayback(context.Background(), "d1") }, request{path: "PUT /v1/me/player/play", device: "d1", hasDevice: true, body: "{}"}},
		{"resume on the active device", func() error { return c.ResumePlayback(context.Background(), "") }, request{path: "PUT /v1/me/player/play", body: "{}"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("request = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSetVolume(t *testing.T) {
	type request struct {
		path, device, volume string
		hasDevice            bool
	}
	var got request
	c := testClient(func(r *http.Request) *http.Response {
		got = request{
			path:      r.Method + " " + r.URL.Path,
			device:    r.URL.Query().Get("device_id"),
			hasDevice: r.URL.Query().Has("device_id"),
			volume:    r.URL.Query().Get("volume_percent"),
		}
		return jsonResponse(r, http.StatusNoContent, ``)
	})

	for _, tt := range []struct {
		name string
		call func() error
		want request
	}{
		{"on a device", func() error { return c.SetVolume(context.Background(), "d1", 55) }, request{path: "PUT /v1/me/player/volume", device: "d1", hasDevice: true, volume: "55"}},
		{"on the active device", func() error { return c.SetVolume(context.Background(), "", 0) }, request{path: "PUT /v1/me/player/volume", volume: "0"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("request = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNowPlayingItem(t *testing.T) {
	tests := []struct {
		name string
		body string
		want NowPlaying
	}{
		{"song", `{"is_playing":true,"progress_ms":170000,"context":{"type":"playlist","uri":"spotify:playlist:a"},"item":{"type":"track","uri":"spotify:track:t","name":"Paranoid","duration_ms":168000,"artists":[{"name":"Black Sabbath"}]}}`,
			NowPlaying{Context: "spotify:playlist:a", Item: "spotify:track:t", Name: "Paranoid", Artist: "Black Sabbath", Progress: 170 * time.Second, Duration: 168 * time.Second}},
		{"episode", `{"is_playing":true,"progress_ms":60000,"context":null,"item":{"type":"episode","uri":"spotify:episode:e","name":"Inside the Final Hours","duration_ms":3600000,"show":{"uri":"spotify:show:s","name":"The Daily"}}}`,
			NowPlaying{Context: "spotify:show:s", Item: "spotify:episode:e", Name: "Inside the Final Hours", Artist: "The Daily", Progress: time.Minute, Duration: time.Hour}},
		{"no item", `{"is_playing":true,"progress_ms":1000,"context":{"type":"show","uri":"spotify:show:s"},"item":null}`,
			NowPlaying{Context: "spotify:show:s"}},
		{"paused", `{"is_playing":false,"progress_ms":1000,"context":{"type":"show","uri":"spotify:show:s"},"item":{"type":"episode","uri":"spotify:episode:e","name":"E","duration_ms":1000}}`,
			NowPlaying{PausedItem: "spotify:episode:e", PausedProgress: time.Second}},
		{"paused without an item", `{"is_playing":false,"progress_ms":1000,"item":null}`,
			NowPlaying{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(func(r *http.Request) *http.Response {
				return jsonResponse(r, http.StatusOK, tt.body)
			})

			got, err := c.NowPlaying(context.Background())

			if err != nil || got != tt.want {
				t.Fatalf("NowPlaying = %+v, %v, want %+v", got, err, tt.want)
			}
		})
	}
}

func savedShowsPage(offset, total int, next bool, items []string) string {
	nextURL := "null"
	if next {
		nextURL = fmt.Sprintf(`"https://api.spotify.com/v1/me/shows?offset=%d"`, offset+50)
	}
	return fmt.Sprintf(`{"items":[%s],"total":%d,"next":%s}`, strings.Join(items, ","), total, nextURL)
}

func savedShowJSON(i int, added time.Time) string {
	return fmt.Sprintf(`{"added_at":%q,"show":{"id":"s%d","uri":"spotify:show:s%d","name":"Show %d"}}`, added.Format(time.RFC3339), i, i, i)
}

func TestAllSavedShowsFollowsShortPages(t *testing.T) {
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	added := func(i int) time.Time { return base.Add(-time.Duration(i) * time.Hour) }
	var offsets []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		offsets = append(offsets, q.Get("offset"))
		if r.URL.Path != "/v1/me/shows" || q.Get("limit") != "50" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		var items []string
		switch q.Get("offset") {
		case "0":
			for i := range 50 {
				switch i {
				case 3:
					items = append(items, `{"added_at":"2026-11-07T10:00:00Z","show":null}`)
				case 10:
					items = append(items, `{"added_at":"2026-11-07T10:00:00Z","show":{"id":"","name":"No ID"}}`)
				case 20:
				default:
					items = append(items, savedShowJSON(i, added(i)))
				}
			}
			return jsonResponse(r, http.StatusOK, savedShowsPage(0, 200, true, items))
		case "50":
			items = append(items, savedShowJSON(49, added(49)))
			for i := 50; i < 60; i++ {
				items = append(items, savedShowJSON(i, added(i)))
			}
			return jsonResponse(r, http.StatusOK, savedShowsPage(50, 200, false, items))
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	shows, err := c.AllSavedShows(context.Background())
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
	if len(shows) != len(want) {
		t.Fatalf("shows = %d, want %d: a null show, a show without an ID or a repeated show was kept", len(shows), len(want))
	}
	for j, i := range want {
		got := shows[j]
		id := spotify.ID(fmt.Sprintf("s%d", i))
		if got.ID != id || got.URI != spotify.URI("spotify:show:"+id) || got.Name != fmt.Sprintf("Show %d", i) || !got.AddedAt.Equal(added(i)) {
			t.Fatalf("shows[%d] = %+v, want s%d in the API order", j, got, i)
		}
	}
}

func TestAllSavedShowsStopsOnAnEmptyPage(t *testing.T) {
	var offsets []string
	c := testClient(func(r *http.Request) *http.Response {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		var items []string
		switch offset {
		case "0":
			for i := range 50 {
				items = append(items, savedShowJSON(i, time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)))
			}
		case "50":
		default:
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		n, _ := strconv.Atoi(offset)
		return jsonResponse(r, http.StatusOK, savedShowsPage(n, 120, true, items))
	})

	shows, err := c.AllSavedShows(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(offsets, ",") != "0,50" {
		t.Fatalf("offsets = %v, want 0,50", offsets)
	}
	if len(shows) != 50 {
		t.Fatalf("shows = %d, want 50", len(shows))
	}
}

func TestAllSavedShowsError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if r.URL.Query().Get("offset") == "0" {
			return jsonResponse(r, http.StatusOK, savedShowsPage(0, 60, true, []string{savedShowJSON(0, time.Now())}))
		}
		return jsonResponse(r, http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`)
	})

	shows, err := c.AllSavedShows(context.Background())

	var apiErr spotify.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.Message != "The access token expired" {
		t.Fatalf("err = %v, want the 401 from the API", err)
	}
	if shows != nil {
		t.Fatalf("shows = %+v, want nil on an error", shows)
	}
}

func sameEpisode(got, want Episode) bool {
	released := got.Released.Equal(want.Released)
	got.Released, want.Released = time.Time{}, time.Time{}
	return released && got == want
}

func TestShowEpisodesPage(t *testing.T) {
	var requests []string
	c := testClient(func(r *http.Request) *http.Response {
		q := r.URL.Query()
		requests = append(requests, q.Get("offset"))
		if r.URL.Path != "/v1/shows/s1/episodes" || q.Get("market") != "from_token" || q.Get("limit") != "50" || q.Has("fields") {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		switch q.Get("offset") {
		case "50":
			return jsonResponse(r, http.StatusOK, `{"total":120,"items":[
				{"id":"e1","uri":"spotify:episode:e1","name":"Day","duration_ms":3600000,"release_date":"2024-11-07","release_date_precision":"day","resume_point":{"fully_played":false,"resume_position_ms":1740000}},
				null,
				{"id":"e2","uri":"spotify:episode:e2","name":"Month","duration_ms":125000,"release_date":"2019-03","release_date_precision":"month","is_playable":false,"resume_point":{"fully_played":true,"resume_position_ms":0}},
				{"id":"e3","uri":"spotify:episode:e3","name":"Year","duration_ms":60000,"release_date":"2018","release_date_precision":"year","is_playable":true},
				{"id":"e4","uri":"spotify:episode:e4","name":"Inferred day","release_date":"2020-02-29"},
				{"id":"e5","uri":"spotify:episode:e5","name":"Inferred month","release_date":"2021-06"},
				{"id":"e6","uri":"spotify:episode:e6","name":"Inferred year","release_date":"2017"},
				{"id":"e7","uri":"spotify:episode:e7","name":"Bad date","release_date":"2020-13-01","release_date_precision":"day"},
				{"id":"e8","uri":"spotify:episode:e8","name":"Unknown precision","release_date":"2020-01-01","release_date_precision":"week"},
				null
			]}`)
		case "100":
			return jsonResponse(r, http.StatusOK, `{"total":102,"items":[{"id":"e9","name":"Nine"},{"id":"e10","name":"Ten"}]}`)
		case "150":
			return jsonResponse(r, http.StatusOK, `{"total":200,"items":[]}`)
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	page, err := c.ShowEpisodesPage(context.Background(), "s1", 50)
	if err != nil {
		t.Fatal(err)
	}

	if page.Next != 60 || page.Total != 120 || page.Last {
		t.Fatalf("page = next %d, total %d, last %v, want 60, 120, false: Next must count the null items", page.Next, page.Total, page.Last)
	}
	want := []Episode{
		{ID: "e1", URI: "spotify:episode:e1", Name: "Day", Duration: time.Hour, Released: time.Date(2024, 11, 7, 0, 0, 0, 0, time.UTC), Precision: "day", Playable: true, Resume: 29 * time.Minute},
		{ID: "e2", URI: "spotify:episode:e2", Name: "Month", Duration: 125 * time.Second, Released: time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC), Precision: "month", FullyPlayed: true},
		{ID: "e3", URI: "spotify:episode:e3", Name: "Year", Duration: time.Minute, Released: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), Precision: "year", Playable: true},
		{ID: "e4", URI: "spotify:episode:e4", Name: "Inferred day", Released: time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC), Precision: "day", Playable: true},
		{ID: "e5", URI: "spotify:episode:e5", Name: "Inferred month", Released: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC), Precision: "month", Playable: true},
		{ID: "e6", URI: "spotify:episode:e6", Name: "Inferred year", Released: time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC), Precision: "year", Playable: true},
		{ID: "e7", URI: "spotify:episode:e7", Name: "Bad date", Playable: true},
		{ID: "e8", URI: "spotify:episode:e8", Name: "Unknown precision", Playable: true},
	}
	if len(page.Items) != len(want) {
		t.Fatalf("items = %+v, want %+v", page.Items, want)
	}
	for i := range want {
		if !sameEpisode(page.Items[i], want[i]) {
			t.Fatalf("items[%d] = %+v, want %+v", i, page.Items[i], want[i])
		}
	}

	page, err = c.ShowEpisodesPage(context.Background(), "s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 102 || !page.Last || len(page.Items) != 2 {
		t.Fatalf("page = next %d, last %v, %d items, want the last page at 102", page.Next, page.Last, len(page.Items))
	}

	page, err = c.ShowEpisodesPage(context.Background(), "s1", 150)
	if err != nil {
		t.Fatal(err)
	}
	if page.Next != 150 || !page.Last || len(page.Items) != 0 {
		t.Fatalf("page = next %d, last %v, %d items, want an empty page to be the last", page.Next, page.Last, len(page.Items))
	}

	if strings.Join(requests, ",") != "50,100,150" {
		t.Fatalf("requests = %v, want 50,100,150", requests)
	}
}

func TestShowEpisodesPageRetriesRateLimits(t *testing.T) {
	attempts := 0
	c := testClient(func(r *http.Request) *http.Response {
		attempts++
		if attempts == 1 {
			resp := jsonResponse(r, http.StatusTooManyRequests, `{"error":{"status":429,"message":"API rate limit exceeded"}}`)
			resp.Header.Set("Retry-After", "0")
			return resp
		}
		return jsonResponse(r, http.StatusOK, `{"total":1,"items":[{"id":"e1","uri":"spotify:episode:e1","name":"One"}]}`)
	})

	page, err := c.ShowEpisodesPage(context.Background(), "s1", 0)
	if err != nil {
		t.Fatal(err)
	}

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "One" || !page.Last {
		t.Fatalf("page = %+v, want the episode from the retry", page)
	}
}

func TestEpisode(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		if r.URL.Query().Get("market") != "from_token" {
			return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
		}
		switch r.URL.Path {
		case "/v1/episodes/e1":
			return jsonResponse(r, http.StatusOK, `{"id":"e1","uri":"spotify:episode:e1","name":"Inside the Final Hours","duration_ms":3600000,"release_date":"2024-11-06","release_date_precision":"day","is_playable":true,"resume_point":{"fully_played":false,"resume_position_ms":2400000}}`)
		case "/v1/episodes/gone":
			return jsonResponse(r, http.StatusNotFound, `{"error":{"status":404,"message":"Non existing id"}}`)
		}
		return jsonResponse(r, http.StatusBadRequest, `{"error":{"status":400,"message":"bad request"}}`)
	})

	got, err := c.Episode(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}

	want := Episode{ID: "e1", URI: "spotify:episode:e1", Name: "Inside the Final Hours", Duration: time.Hour, Released: time.Date(2024, 11, 6, 0, 0, 0, 0, time.UTC), Precision: "day", Playable: true, Resume: 40 * time.Minute}
	if !sameEpisode(got, want) {
		t.Fatalf("Episode = %+v, want %+v", got, want)
	}

	if _, err := c.Episode(context.Background(), "gone"); err == nil {
		t.Fatal("a 404 did not return an error")
	}
}

func TestPlayEpisode(t *testing.T) {
	var path, deviceID, body string
	var hasDevice bool
	c := testClient(func(r *http.Request) *http.Response {
		b, _ := io.ReadAll(r.Body)
		path, body = r.Method+" "+r.URL.Path, strings.TrimSpace(string(b))
		deviceID, hasDevice = r.URL.Query().Get("device_id"), r.URL.Query().Has("device_id")
		return jsonResponse(r, http.StatusNoContent, ``)
	})

	if err := c.PlayEpisode(context.Background(), "d1", "spotify:show:s1", "spotify:episode:e1", 0); err != nil {
		t.Fatal(err)
	}
	if path != "PUT /v1/me/player/play" {
		t.Fatalf("request = %s, want PUT /v1/me/player/play", path)
	}
	if want := `{"context_uri":"spotify:show:s1","offset":{"uri":"spotify:episode:e1"}}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
	if deviceID != "d1" {
		t.Fatalf("device_id = %q, want d1", deviceID)
	}

	if err := c.PlayEpisode(context.Background(), "", "spotify:show:s1", "spotify:episode:e1", 29*time.Minute); err != nil {
		t.Fatal(err)
	}
	if want := `{"context_uri":"spotify:show:s1","offset":{"uri":"spotify:episode:e1"},"position_ms":1740000}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
	if hasDevice {
		t.Fatalf("device_id = %q, want no device_id without a device", deviceID)
	}
}

func TestEpisodeInProgress(t *testing.T) {
	if e := (Episode{Duration: time.Hour, Resume: 29 * time.Minute}); !e.InProgress() {
		t.Fatalf("%+v is not in progress", e)
	}
	for _, e := range []Episode{
		{Duration: time.Hour, Resume: 29 * time.Minute, FullyPlayed: true},
		{Duration: time.Hour},
		{Duration: time.Hour, Resume: time.Hour},
		{Duration: time.Hour, Resume: 2 * time.Hour},
	} {
		if e.InProgress() {
			t.Fatalf("%+v is in progress", e)
		}
	}
}

type tokens struct {
	mu    sync.Mutex
	next  []*oauth2.Token
	calls int
}

func (s *tokens) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.next) == 0 {
		return nil, errors.New("refresh token revoked")
	}
	t := s.next[0]
	if len(s.next) > 1 {
		s.next = s.next[1:]
	}
	return t, nil
}

func TestRefreshedTokensAreSaved(t *testing.T) {
	first := &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"}
	second := &oauth2.Token{AccessToken: "a2", RefreshToken: "r2"}
	src := &tokens{next: []*oauth2.Token{first, first, second, second}}
	var saved []string
	s := &savingTokenSource{src: src, accessToken: first.AccessToken, save: func(t *oauth2.Token) error {
		saved = append(saved, t.RefreshToken)
		return errors.New("disk full")
	}}

	for range 4 {
		if _, err := s.Token(); err != nil {
			t.Fatal(err)
		}
	}

	if strings.Join(saved, ",") != "r2" {
		t.Fatalf("saved = %v, want only the refreshed token", saved)
	}

	src.next = nil
	if _, err := s.Token(); err == nil {
		t.Fatal("a failed refresh did not return the error")
	}
}
