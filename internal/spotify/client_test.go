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

	want := []PlayedPlaylist{
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

func TestPlayingPlaylist(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   spotify.URI
		err    bool
	}{
		{"playlist", http.StatusOK, `{"is_playing":true,"context":{"type":"playlist","uri":"spotify:playlist:a"}}`, "spotify:playlist:a", false},
		{"paused", http.StatusOK, `{"is_playing":false,"context":{"type":"playlist","uri":"spotify:playlist:a"}}`, "", false},
		{"album", http.StatusOK, `{"is_playing":true,"context":{"type":"album","uri":"spotify:album:a"}}`, "", false},
		{"no context", http.StatusOK, `{"is_playing":true,"context":null}`, "", false},
		{"nothing plays", http.StatusNoContent, ``, "", false},
		{"error", http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(func(r *http.Request) *http.Response {
				if r.URL.Path != "/v1/me/player" {
					return jsonResponse(r, http.StatusNotFound, `{"error":{"status":404,"message":"not found"}}`)
				}
				return jsonResponse(r, tt.status, tt.body)
			})

			got, err := c.PlayingPlaylist(context.Background())

			if got != tt.want || (err != nil) != tt.err {
				t.Fatalf("PlayingPlaylist = %q, %v, want %q, error %v", got, err, tt.want, tt.err)
			}
		})
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
