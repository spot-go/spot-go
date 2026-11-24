package spotify

import (
	"context"
	"crypto/rand"
	_ "embed"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/config"
	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"golang.org/x/oauth2"
)

type (
	ClientResponse struct {
		Client *Client
		Err    error
	}

	// An Authorization logs in to Spotify in the browser, with the Spotify
	// app of ClientID: Start it, open URL in the browser and Wait.
	Authorization struct {
		// ClientID is the Client ID of the Spotify app the login is for.
		ClientID string
		// URL is Spotify's login page, to open in the browser.
		URL string

		config   *oauth2.Config
		state    string
		verifier string
		end      sync.Once
		done     chan struct{}
		result   ClientResponse
	}

	savingTokenSource struct {
		mu          sync.Mutex
		src         oauth2.TokenSource
		accessToken string
		save        func(*oauth2.Token) error
	}
)

const (
	callbackAddr = "127.0.0.1:10888"
	// RedirectURI is where Spotify sends the browser back to spot-go after
	// the login. The Spotify app spot-go logs in with must have this exact
	// redirect URI.
	RedirectURI = "http://" + callbackAddr + "/callback"
	// DashboardURL is where Spotify apps are created and set up.
	DashboardURL = "https://developer.spotify.com/dashboard"
	// ClientIDEnv names the environment variable with the Client ID of the
	// Spotify app to log in with.
	ClientIDEnv = "SPOT_GO_CLIENT_ID"

	scopeUserReadPlaybackPosition = "user-read-playback-position"

	// exchangeTimeout is how long Spotify can take to give the token of a
	// login.
	exchangeTimeout = 30 * time.Second
)

// DefaultClientID is the Client ID of spot-go's own Spotify app, which
// spot-go logs in with when the user gives no Client ID. The app is in
// development mode, so Spotify only lets the accounts on its user list use
// it. A build can set another one, or none, with
//
//	-ldflags "-X github.com/spot-go/spot-go/internal/spotify.DefaultClientID=..."
var DefaultClientID = "573247e4a8b84ca8842f2aca8b1f3878"

var (
	scopes = []string{
		spotifyauth.ScopeUserReadPrivate,
		spotifyauth.ScopeUserReadEmail,
		spotifyauth.ScopeUserReadPlaybackState,
		spotifyauth.ScopeUserModifyPlaybackState,
		spotifyauth.ScopeUserReadCurrentlyPlaying,
		spotifyauth.ScopePlaylistReadPrivate,
		spotifyauth.ScopeUserReadRecentlyPlayed,
		spotifyauth.ScopeUserLibraryRead,
		scopeUserReadPlaybackPosition,
		spotifyauth.ScopeUserFollowRead,
		spotifyauth.ScopeUserLibraryModify,
	}
	// endpoint is Spotify's login. A login with PKCE has no client secret,
	// so the token requests carry the Client ID in their body.
	endpoint = oauth2.Endpoint{
		AuthURL:   spotifyauth.AuthURL,
		TokenURL:  spotifyauth.TokenURL,
		AuthStyle: oauth2.AuthStyleInParams,
	}

	// callback is the server Spotify sends the browser back to, and the
	// login it waits for. It starts with the first login, and the logins
	// after it take its place.
	callback struct {
		mu        sync.Mutex
		listening bool
		pending   *Authorization
	}

	// saveAuth saves a new login.
	saveAuth = config.SaveAuth

	//go:embed static/auth_success.html
	authSuccessPage string
	//go:embed static/auth_error.html
	authErrorPage string

	ErrNeedsAuth = errors.New("needs to authorize")
	// ErrCanceled ends the wait of an Authorization canceled, or replaced by
	// a newer one.
	ErrCanceled = errors.New("the login was canceled")
)

// ValidClientID reports whether id has the form of a Client ID: 32
// hexadecimal digits, as the Spotify Developer Dashboard shows it.
func ValidClientID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ClientIDFromEnv returns the Client ID set in the SPOT_GO_CLIENT_ID
// environment variable, or "" when it is not set.
func ClientIDFromEnv() string {
	return strings.TrimSpace(os.Getenv(ClientIDEnv))
}

// LoggedInClientID returns the Client ID of the Spotify app of the login
// spot-go saved, or "" when it saved none.
func LoggedInClientID() string {
	s := config.AppSettings
	if s == nil || s.Auth == nil || s.Auth.Token == nil {
		return ""
	}
	if s.Auth.ClientID != "" {
		return s.Auth.ClientID
	}
	// spot-go saved its logins without their app before it logged in
	// with other apps than its own.
	return DefaultClientID
}

func oauthConfig(clientID string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    clientID,
		Endpoint:    endpoint,
		RedirectURL: RedirectURI,
		Scopes:      scopes,
	}
}

// NewAuthorization returns a login with the Spotify app of clientID.
func NewAuthorization(clientID string) *Authorization {
	cfg := oauthConfig(clientID)
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	return &Authorization{
		ClientID: clientID,
		URL:      cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)),
		config:   cfg,
		state:    state,
		verifier: verifier,
		done:     make(chan struct{}),
	}
}

// Start waits for Spotify to send the browser back to spot-go with the
// login of a, in place of any login started before, which ends with
// ErrCanceled.
func (a *Authorization) Start() error {
	callback.mu.Lock()
	defer callback.mu.Unlock()

	select {
	case <-a.done:
		// Canceled before it started.
		return ErrCanceled
	default:
	}
	if !callback.listening {
		ln, err := net.Listen("tcp", callbackAddr)
		if err != nil {
			return fmt.Errorf("fail to wait for the login on %s: %w", callbackAddr, err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/callback", completeAuth)
		server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go server.Serve(ln)
		callback.listening = true
	}
	a.replacePending()
	return nil
}

// replacePending makes a the login the callback waits for, in place of the
// one it waited for, which ends with ErrCanceled. callback.mu must be held.
func (a *Authorization) replacePending() {
	if p := callback.pending; p != nil && p != a {
		p.finish(ClientResponse{Err: ErrCanceled})
	}
	callback.pending = a
}

// Wait returns the client of the login once it ends, or ErrCanceled once
// it is canceled.
func (a *Authorization) Wait() ClientResponse {
	<-a.done
	return a.result
}

// Cancel ends the login: Wait returns ErrCanceled, unless the login ended
// before.
func (a *Authorization) Cancel() {
	callback.mu.Lock()
	if callback.pending == a {
		callback.pending = nil
	}
	callback.mu.Unlock()
	a.finish(ClientResponse{Err: ErrCanceled})
}

// finish ends the login with r, unless it ended before.
func (a *Authorization) finish(r ClientResponse) {
	a.complete(func() ClientResponse { return r })
}

// complete ends the login with what end returns, unless it ended before:
// then end doesn't run, and complete returns false. Cancel waits for end.
func (a *Authorization) complete(end func() ClientResponse) bool {
	ran := false
	a.end.Do(func() {
		ran = true
		a.result = end()
		close(a.done)
	})
	return ran
}

func Login(ctx context.Context) ClientResponse {
	s := config.AppSettings
	if s == nil || s.NeedsAuthenticateWebSpotify(scopes) {
		return ClientResponse{nil, ErrNeedsAuth}
	}
	clientID := LoggedInClientID()
	if clientID == "" {
		return ClientResponse{nil, ErrNeedsAuth}
	}
	authSettings := config.AppSettings.Auth
	c := newSessionClient(ctx, oauthConfig(clientID), authSettings.Token)

	// PKCE allows only one refresh per access_token
	// The code below saves the new token (thus the new access token) after a refresh
	if !authSettings.Token.Valid() {
		newToken, err := c.Token()
		if err != nil {
			if loginEnded(err) {
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

// loginEnded reports whether err, an error refreshing the token, says the
// login no longer works, so that only a new login can: Spotify revoked the
// token or let it expire, or the app it belongs to is gone.
func loginEnded(err error) bool {
	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) {
		return false
	}
	return retrieveErr.ErrorCode == "invalid_grant" ||
		retrieveErr.ErrorCode == "invalid_client" ||
		retrieveErr.ErrorDescription == "Refresh token revoked"
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	errorPage := func(msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, authErrorPage, html.EscapeString(msg))
	}

	query := r.URL.Query()
	callback.mu.Lock()
	a := callback.pending
	if a != nil && query.Get("state") == a.state {
		// The login ends here: the same page again, as when the browser
		// reloads it, doesn't log in again.
		callback.pending = nil
	} else {
		a = nil
	}
	callback.mu.Unlock()
	if a == nil {
		errorPage("This login is not going on in spot-go anymore. Log in again from spot-go.")
		return
	}

	fail := func(err error) {
		errorPage(err.Error())
		a.finish(ClientResponse{nil, err})
	}
	if e := query.Get("error"); e != "" {
		fail(fmt.Errorf("Spotify did not log you in: %s", e))
		return
	}
	code := query.Get("code")
	if code == "" {
		fail(errors.New("Spotify sent no authorization code"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), exchangeTimeout)
	defer cancel()
	t, err := a.config.Exchange(ctx, code, oauth2.VerifierOption(a.verifier))
	if err != nil {
		fail(fmt.Errorf("fail to get token: %w", err))
		return
	}

	// A login canceled meanwhile, as when spot-go went back to the Client
	// ID, isn't saved: another login may have taken its place.
	var saveErr error
	completed := a.complete(func() ClientResponse {
		if err := saveAuth(t, scopes, a.ClientID); err != nil {
			saveErr = fmt.Errorf("fail to save config: %w", err)
			return ClientResponse{nil, saveErr}
		}
		return ClientResponse{newSessionClient(context.Background(), a.config, t), nil}
	})
	switch {
	case !completed:
		errorPage("This login was canceled in spot-go. Log in again from spot-go.")
	case saveErr != nil:
		errorPage(saveErr.Error())
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, authSuccessPage)
	}
}

// newSessionClient is a client that refreshes the token t of the app of
// cfg, with the HTTP client of ctx, when it has one (oauth2.HTTPClient),
// and saves it. The timeout of that client stays on the refresh: Web API
// requests end with their context.
func newSessionClient(ctx context.Context, cfg *oauth2.Config, t *oauth2.Token) *Client {
	httpClient := cfg.Client(ctx, t)
	httpClient.Timeout = 0
	if transport, ok := httpClient.Transport.(*oauth2.Transport); ok {
		transport.Source = &savingTokenSource{src: transport.Source, accessToken: t.AccessToken, save: config.SaveToken}
	}
	return NewClient(httpClient)
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if t.AccessToken != s.accessToken {
		s.accessToken = t.AccessToken
		_ = s.save(t)
	}
	return t, nil
}
