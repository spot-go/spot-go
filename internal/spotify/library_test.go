package spotify

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/zmb3/spotify/v2"
)

func TestInLibrary(t *testing.T) {
	for _, tt := range []struct {
		item spotify.URI
		body string
		want bool
	}{
		{"spotify:track:so-what", "[true]", true},
		{"spotify:track:so-what", "[false]", false},
		{"spotify:episode:e1", "[true]", true},
	} {
		var got []string
		c := testClient(func(r *http.Request) *http.Response {
			got = append(got, r.Method+" "+r.URL.Path+" "+r.URL.Query().Get("uris"))
			return jsonResponse(r, http.StatusOK, tt.body)
		})

		saved, err := c.InLibrary(context.Background(), tt.item)
		if err != nil {
			t.Fatal(err)
		}
		if saved != tt.want {
			t.Errorf("InLibrary(%s) with %s = %t, want %t", tt.item, tt.body, saved, tt.want)
		}
		if want := []string{"GET /v1/me/library/contains " + string(tt.item)}; !slices.Equal(got, want) {
			t.Errorf("requests = %q, want %q", got, want)
		}
	}
}

func TestInLibraryWithoutOneAnswerFails(t *testing.T) {
	for _, body := range []string{"[]", "[true,false]"} {
		c := testClient(func(r *http.Request) *http.Response {
			return jsonResponse(r, http.StatusOK, body)
		})
		if _, err := c.InLibrary(context.Background(), "spotify:track:so-what"); err == nil {
			t.Errorf("InLibrary with %s: no error", body)
		}
	}

	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusNoContent, "")
	})
	if _, err := c.InLibrary(context.Background(), "spotify:track:so-what"); err == nil {
		t.Error("InLibrary with no content: no error")
	}
}

func TestInLibraryError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
	})

	_, err := c.InLibrary(context.Background(), "spotify:track:so-what")
	var apiErr spotify.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Message != "Insufficient client scope" {
		t.Fatalf("err = %v, want the 403 of Spotify", err)
	}
}

func TestAddToAndRemoveFromLibrary(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusCreated} {
		var got []string
		c := testClient(func(r *http.Request) *http.Response {
			got = append(got, r.Method+" "+r.URL.Path+" "+r.URL.Query().Get("uris"))
			return jsonResponse(r, status, "")
		})

		if err := c.AddToLibrary(context.Background(), "spotify:track:so-what"); err != nil {
			t.Fatalf("AddToLibrary with %d: %v", status, err)
		}
		if err := c.RemoveFromLibrary(context.Background(), "spotify:episode:e1"); err != nil {
			t.Fatalf("RemoveFromLibrary with %d: %v", status, err)
		}
		want := []string{"PUT /v1/me/library spotify:track:so-what", "DELETE /v1/me/library spotify:episode:e1"}
		if !slices.Equal(got, want) {
			t.Fatalf("requests = %q, want %q", got, want)
		}
	}
}

func TestChangeLibraryError(t *testing.T) {
	c := testClient(func(r *http.Request) *http.Response {
		return jsonResponse(r, http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
	})

	for _, change := range []func(context.Context, spotify.URI) error{c.AddToLibrary, c.RemoveFromLibrary} {
		err := change(context.Background(), "spotify:track:so-what")
		var apiErr spotify.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Message != "Insufficient client scope" {
			t.Fatalf("err = %v, want the 403 of Spotify", err)
		}
	}
}

func TestChangeLibraryWaitsForTheRateLimit(t *testing.T) {
	var got []string
	c := testClient(func(r *http.Request) *http.Response {
		got = append(got, r.Method)
		if len(got) == 1 {
			resp := jsonResponse(r, http.StatusTooManyRequests, `{"error":{"status":429,"message":"API rate limit exceeded"}}`)
			resp.Header.Set("Retry-After", "0")
			return resp
		}
		return jsonResponse(r, http.StatusOK, "")
	})

	if err := c.AddToLibrary(context.Background(), "spotify:track:so-what"); err != nil {
		t.Fatal(err)
	}
	if want := []string{http.MethodPut, http.MethodPut}; !slices.Equal(got, want) {
		t.Fatalf("requests = %q, want %q", got, want)
	}
}
