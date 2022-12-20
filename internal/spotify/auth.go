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

type clientResponse struct {
	client *spotify.Client
	err    error
}

const (
	localPort      = ":10888"
	ClientID       = "573247e4a8b84ca8842f2aca8b1f3878"
	allowedCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123456_.-~"
)

var (
	scopes = []string{
		spotifyauth.ScopeUserReadPrivate,
		spotifyauth.ScopeUserReadPlaybackState,
		spotifyauth.ScopeUserModifyPlaybackState,
		spotifyauth.ScopeUserReadCurrentlyPlaying,
	}
	redirectURI = fmt.Sprintf("http://localhost%s/callback", localPort)
	auth        = spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURI),
		spotifyauth.WithScopes(scopes...),
		spotifyauth.WithClientID(ClientID),
	)
	clientResp    = make(chan clientResponse)
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
		sb.WriteByte(allowedCharset[rand.Intn(len(allowedCharset))])
	}
	return sb.String()
}

func authorize() (*spotify.Client, error) {
	http.HandleFunc("/callback", completeAuth)
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

	res := <-clientResp
	return res.client, res.err
}

func Login(ctx context.Context) (*spotify.Client, error) {
	s := config.AppSettings
	if s == nil || s.ShouldAuthenticateSpotify(scopes) {
		return authorize()
	}
	authSettings := config.AppSettings.Auth
	c := spotify.New(auth.Client(ctx, authSettings.Token))

	// PKCE allows only one refresh per access_token
	// The code below saves the new token (this the new access token) after a refresh
	if !authSettings.Token.Valid() {
		newToken, err := c.Token()
		if err != nil {
			if err != nil &&
				strings.Contains(err.Error(), `"error_description":"Refresh token revoked"`) {
				return authorize()
			}
			return nil, fmt.Errorf("fail to get new token: %+v", err)
		}
		authSettings.Token = newToken
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
		fmt.Fprint(w, "Fail to get token: ", err)
		clientResp <- clientResponse{nil, fmt.Errorf("fail to get token: %s", err)}
		return
	}
	if st := r.FormValue("state"); st != state {
		fmt.Fprintf(w, "State mismatch: %s != %s", st, state)
		clientResp <- clientResponse{nil, fmt.Errorf("state mismatch: %s", err)}
		return
	}

	config.AppSettings.Auth.Token = t
	config.AppSettings.Auth.Scopes = scopes
	err = config.AppSettings.Save()
	if err != nil {
		log.Fatalf("fail to save config: %+v\n", err)
	}

	client := spotify.New(auth.Client(r.Context(), t))
	fmt.Fprint(w, "Login Completed. You can can close this window and go back to the terminal")
	clientResp <- clientResponse{client, nil}
}
