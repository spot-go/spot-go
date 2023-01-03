package spotify

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/spot-go/spot-go/internal/browser"
	"github.com/spot-go/spot-go/internal/config"
	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"golang.org/x/oauth2"
)

type (
	ClientResponse struct {
		Client *Client
		Err    error
	}
)

const (
	localPort      = ":10888"
	ClientID       = "573247e4a8b84ca8842f2aca8b1f3878"
	allowedCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123456_.-~"
)

var (
	scopes = []string{
		spotifyauth.ScopeUserReadPrivate,
		spotifyauth.ScopeUserReadEmail,
		spotifyauth.ScopeUserReadPlaybackState,
		spotifyauth.ScopeUserModifyPlaybackState,
		spotifyauth.ScopeUserReadCurrentlyPlaying,
		spotifyauth.ScopePlaylistReadPrivate,
	}
	redirectURI = fmt.Sprintf("http://localhost%s/callback", localPort)
	auth        = spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURI),
		spotifyauth.WithScopes(scopes...),
		spotifyauth.WithClientID(ClientID),
	)
	clientResp    = make(chan ClientResponse)
	state         = randomString(16)
	codeVerifier  = randomString(128)
	codeChallenge = computeHmac256(codeVerifier)
	AuthURL       = auth.AuthURL(state,
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
	)

	server *http.Server

	//go:embed static/auth_success.html
	authSuccessPage string
	//go:embed static/auth_error.html
	authErrorPage string
	ErrNeedsAuth  = errors.New("needs to authorize")
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

func Authorize() ClientResponse {
	http.HandleFunc("/callback", completeAuth)
	server = &http.Server{Addr: localPort}
	go server.ListenAndServe()

	err := browser.Open(AuthURL)
	if err != nil {
		return ClientResponse{nil, fmt.Errorf("fail to open browser: %+v", err)}
	}

	return <-clientResp
}

func Login(ctx context.Context) ClientResponse {
	s := config.AppSettings
	if s == nil || s.NeedsAuthenticateWebSpotify(scopes) {
		return ClientResponse{nil, ErrNeedsAuth}
	}
	authSettings := config.AppSettings.Auth
	c := NewClient(auth.Client(ctx, authSettings.Token))

	// PKCE allows only one refresh per access_token
	// The code below saves the new token (thus the new access token) after a refresh
	if !authSettings.Token.Valid() {
		newToken, err := c.Token()
		if err != nil {
			if err != nil &&
				strings.Contains(err.Error(), `"error_description":"Refresh token revoked"`) {
				return ClientResponse{nil, ErrNeedsAuth}
			}
			return ClientResponse{nil, fmt.Errorf("fail to get new token: %+v", err)}
		}
		authSettings.Token = newToken
		err = s.Save()
		if err != nil {
			return ClientResponse{nil, fmt.Errorf("fail to save config: %+v", err)}
		}
	}
	return ClientResponse{c, nil}
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	errorPage := func(err error) {
		msg := fmt.Sprintf(authErrorPage, err.Error())
		fmt.Fprint(w, msg)
	}

	t, err := auth.Token(r.Context(), state, r,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		errorPage(err)
		clientResp <- ClientResponse{nil, fmt.Errorf("fail to get token: %s", err)}
		return
	}
	if st := r.FormValue("state"); st != state {
		err := fmt.Errorf("state mismatch: %s != %s", st, state)
		errorPage(err)
		fmt.Fprintf(w, "State mismatch: %s != %s", st, state)
		clientResp <- ClientResponse{nil, err}
		return
	}

	config.AppSettings.Auth.Token = t
	config.AppSettings.Auth.Scopes = scopes
	err = config.AppSettings.Save()
	if err != nil {
		log.Fatalf("fail to save config: %+v\n", err)
	}

	client := NewClient(auth.Client(r.Context(), t))
	fmt.Fprint(w, authSuccessPage)
	clientResp <- ClientResponse{client, nil}
}
