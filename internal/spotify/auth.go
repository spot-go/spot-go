package spotify

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/spot-go/spot-go/internal/browser"
	"github.com/spot-go/spot-go/internal/config"
	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

// redirectURI is the OAuth redirect URI for the application.
// You must register an application at Spotify's developer portal
// and enter this value.
const (
	localPort = ":10888"
	ClientID  = "573247e4a8b84ca8842f2aca8b1f3878"
	charset   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123456_.-~"
)

var (
	redirectURI = fmt.Sprintf("http://localhost%s/callback", localPort)
	auth        = spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURI),
		spotifyauth.WithScopes(spotifyauth.ScopeUserReadPrivate),
		spotifyauth.WithClientID(ClientID),
	)
	ch            = make(chan *spotify.Client)
	state         = randomString(16)
	codeVerifier  = randomString(128)
	codeChallenge = computeHmac256(codeVerifier)
	server        *http.Server
)

func computeHmac256(message string) string {
	h := sha256.New()
	_, err := h.Write([]byte(message))
	if err != nil {
		panic(err)
	}
	v := base64.StdEncoding.EncodeToString(h.Sum(nil))
	v = strings.ReplaceAll(v, "=", "")
	v = strings.ReplaceAll(v, "+", "-")
	return strings.ReplaceAll(v, "/", "_")
}

func randomString(n int) string {
	sb := strings.Builder{}
	sb.Grow(n)
	for i := 0; i < n; i++ {
		sb.WriteByte(charset[rand.Intn(len(charset))])
	}
	return sb.String()
}

func authorize() (*spotify.Client, error) {
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	server = &http.Server{Addr: localPort}
	go server.ListenAndServe()

	url := auth.AuthURL(state,
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
	)
	err := browser.Open(url)
	if err != nil {
		return nil, fmt.Errorf("fail to open browser: %+v", err)
	}

	// wait for auth to complete
	return <-ch, nil
}

func Login(ctx context.Context) (*spotify.Client, error) {
	s := config.AppSettings
	if s.Token == nil {
		return authorize()
	}

	c := spotify.New(auth.Client(ctx, s.Token))

	// PKCE allows only one refresh per access_token
	// The code above saves the new token (this the new access token) after a refresh
	if !s.Token.Valid() {
		newToken, err := c.Token()
		if err != nil {
			if err != nil &&
				strings.Contains(err.Error(), `"error_description":"Refresh token revoked"`) {
				return authorize()
			}
			return nil, fmt.Errorf("fail to get new token: %+v", err)
		}
		s.Token = newToken
		err = s.Save()
		if err != nil {
			return nil, fmt.Errorf("fail to save config: %+v", err)
		}
	}
	return c, nil
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	t, err := auth.Token(r.Context(), state, r,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		fmt.Fprintln(w, "Couldn't get token:", err)
		go server.Shutdown(context.Background())
		ch <- nil
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	s := config.AppSettings
	s.Token = t
	err = s.Save()
	if err != nil {
		log.Fatalf("fail to save config: %+v\n", err)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), t))
	fmt.Fprintf(w, "Login Completed. You can can close this window and go back to the terminal")
	go server.Shutdown(context.Background())
	ch <- client
}
