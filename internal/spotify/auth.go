package spotify

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/spot-go/spot-go/internal/browser"
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
	ch    = make(chan *spotify.Client)
	state = "abc123"
	// These should be randomly generated for each request
	//  More information on generating these can be found here,
	// https://www.oauth.com/playground/authorization-code-with-pkce.html
	codeVerifier  = randomString(128)
	codeChallenge = computeHmac256(codeVerifier)
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

func Login() (*spotify.Client, error) {
	fmt.Printf("codeVerifier %+v\n", codeVerifier)
	fmt.Printf("codeChallenge %+v\n", codeChallenge)
	// first start an HTTP server
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go http.ListenAndServe(localPort, nil)

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

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}
	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok))
	fmt.Fprintf(w, "Login Completed!")
	ch <- client
}
