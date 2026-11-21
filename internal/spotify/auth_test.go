package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/config"

	"golang.org/x/oauth2"
)

const ownClientID = "0123456789abcdef0123456789abcdef"

func TestValidClientID(t *testing.T) {
	cases := map[string]bool{
		DefaultClientID:                     true,
		ownClientID:                         true,
		"":                                  false,
		"0123456789abcdef0123456789abcde":   false,
		"0123456789abcdef0123456789abcdef0": false,
		"0123456789ABCDEF0123456789ABCDEF":  false,
		"0123456789abcdef0123456789abcdeg":  false,
		" 0123456789abcdef0123456789abcde":  false,
	}
	for id, want := range cases {
		if got := ValidClientID(id); got != want {
			t.Errorf("ValidClientID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestClientIDFromEnv(t *testing.T) {
	t.Setenv(ClientIDEnv, " "+ownClientID+"\n")
	if got := ClientIDFromEnv(); got != ownClientID {
		t.Fatalf("ClientIDFromEnv() = %q, want %q", got, ownClientID)
	}
	t.Setenv(ClientIDEnv, "")
	if got := ClientIDFromEnv(); got != "" {
		t.Fatalf("ClientIDFromEnv() = %q with the variable empty", got)
	}
}

// withSettings makes s the settings spot-go loaded, for the test.
func withSettings(t *testing.T, s *config.Settings) {
	t.Helper()
	old := config.AppSettings
	config.AppSettings = s
	t.Cleanup(func() { config.AppSettings = old })
}

func TestLoggedInClientID(t *testing.T) {
	token := &oauth2.Token{AccessToken: "a", RefreshToken: "r"}
	cases := []struct {
		name     string
		settings *config.Settings
		want     string
	}{
		{"no settings", nil, ""},
		{"no login", &config.Settings{}, ""},
		{"no token", &config.Settings{Auth: &config.Auth{ClientID: ownClientID}}, ""},
		{"login of an older spot-go", &config.Settings{Auth: &config.Auth{Token: token}}, DefaultClientID},
		{"login with another app", &config.Settings{Auth: &config.Auth{Token: token, ClientID: ownClientID}}, ownClientID},
	}
	for _, c := range cases {
		withSettings(t, c.settings)
		if got := LoggedInClientID(); got != c.want {
			t.Errorf("%s: LoggedInClientID() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAuthorizationURL(t *testing.T) {
	a := NewAuthorization(ownClientID)
	u, err := url.Parse(a.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://accounts.spotify.com/authorize" {
		t.Errorf("login page = %s, want Spotify's", got)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":             ownClientID,
		"redirect_uri":          RedirectURI,
		"response_type":         "code",
		"code_challenge_method": "S256",
		"code_challenge":        oauth2.S256ChallengeFromVerifier(a.verifier),
		"state":                 a.state,
		"scope":                 strings.Join(scopes, " "),
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if a.state == "" || a.verifier == "" {
		t.Fatal("the login has no state or no code verifier")
	}

	b := NewAuthorization(ownClientID)
	if b.state == a.state || b.verifier == a.verifier {
		t.Fatal("two logins share their state or code verifier")
	}
}

// tokenServer is Spotify's token endpoint, for the test: it answers with
// respond, and keeps the form of each request.
func tokenServer(t *testing.T, respond func(w http.ResponseWriter, form url.Values)) *[]url.Values {
	t.Helper()
	var forms []url.Values
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("token request: %v", err)
		}
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("a token request without a client secret sent basic auth")
		}
		forms = append(forms, r.PostForm)
		respond(w, r.PostForm)
	}))
	t.Cleanup(s.Close)

	old := endpoint
	endpoint.TokenURL = s.URL
	t.Cleanup(func() { endpoint = old })
	return &forms
}

func writeToken(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    3600,
	})
}

func writeTokenError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

type savedAuth struct {
	token    *oauth2.Token
	scopes   []string
	clientID string
}

// fakeSaveAuth keeps the logins saved, in place of the settings file.
func fakeSaveAuth(t *testing.T, err error) *[]savedAuth {
	t.Helper()
	var saved []savedAuth
	old := saveAuth
	saveAuth = func(token *oauth2.Token, scopes []string, clientID string) error {
		saved = append(saved, savedAuth{token, scopes, clientID})
		return err
	}
	t.Cleanup(func() { saveAuth = old })
	return &saved
}

// pending makes a the login the callback waits for, without listening.
func pending(t *testing.T, a *Authorization) {
	t.Helper()
	callback.mu.Lock()
	a.replacePending()
	callback.mu.Unlock()
	t.Cleanup(a.Cancel)
}

func callbackRequest(query url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/callback?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	completeAuth(w, r)
	return w
}

func ended(a *Authorization) (ClientResponse, bool) {
	select {
	case <-a.done:
		return a.Wait(), true
	default:
		return ClientResponse{}, false
	}
}

func TestCallbackLogsInWithTheAppOfTheLogin(t *testing.T) {
	forms := tokenServer(t, func(w http.ResponseWriter, form url.Values) {
		writeToken(w, "access", "refresh")
	})
	saved := fakeSaveAuth(t, nil)
	a := NewAuthorization(ownClientID)
	pending(t, a)

	w := callbackRequest(url.Values{"code": {"the-code"}, "state": {a.state}})

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Authentication complete") {
		t.Fatalf("callback answered %d:\n%s", w.Code, w.Body)
	}
	resp, ok := ended(a)
	if !ok || resp.Err != nil || resp.Client == nil {
		t.Fatalf("the login ended with %+v (ended: %v), want a client", resp, ok)
	}
	if len(*forms) != 1 {
		t.Fatalf("%d token requests, want 1", len(*forms))
	}
	form := (*forms)[0]
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "the-code",
		"code_verifier": a.verifier,
		"client_id":     ownClientID,
		"redirect_uri":  RedirectURI,
	}
	for k, v := range want {
		if got := form.Get(k); got != v {
			t.Errorf("token request %s = %q, want %q", k, got, v)
		}
	}
	if form.Has("client_secret") {
		t.Error("the token request sent a client secret")
	}
	if len(*saved) != 1 || (*saved)[0].clientID != ownClientID || (*saved)[0].token.AccessToken != "access" ||
		strings.Join((*saved)[0].scopes, " ") != strings.Join(scopes, " ") {
		t.Fatalf("saved %+v, want the token of %s with the scopes", *saved, ownClientID)
	}

	// The browser loads the page again.
	w = callbackRequest(url.Values{"code": {"the-code"}, "state": {a.state}})
	if w.Code != http.StatusBadRequest || len(*forms) != 1 {
		t.Fatalf("the same callback again answered %d after %d token requests, want 400 after 1", w.Code, len(*forms))
	}
}

func TestCallbackOfAnotherLoginIsRefused(t *testing.T) {
	forms := tokenServer(t, func(w http.ResponseWriter, form url.Values) {
		writeToken(w, "access", "refresh")
	})
	fakeSaveAuth(t, nil)
	a := NewAuthorization(ownClientID)
	pending(t, a)

	w := callbackRequest(url.Values{"code": {"the-code"}, "state": {"another"}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not going on") {
		t.Fatalf("callback with another state answered %d:\n%s", w.Code, w.Body)
	}
	if _, ok := ended(a); ok {
		t.Fatal("a callback with another state ended the login")
	}
	if len(*forms) != 0 {
		t.Fatal("a callback with another state asked for a token")
	}

	a.Cancel()
	w = callbackRequest(url.Values{"code": {"the-code"}, "state": {a.state}})
	if w.Code != http.StatusBadRequest || len(*forms) != 0 {
		t.Fatalf("callback of a canceled login answered %d", w.Code)
	}
}

func TestCallbackWithAnErrorEndsTheLogin(t *testing.T) {
	tokenServer(t, func(w http.ResponseWriter, form url.Values) {
		t.Error("a refused login asked for a token")
	})
	saved := fakeSaveAuth(t, nil)
	a := NewAuthorization(ownClientID)
	pending(t, a)

	w := callbackRequest(url.Values{"error": {"<b>access_denied</b>"}, "state": {a.state}})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("callback answered %d, want 400", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "<b>access_denied") || !strings.Contains(body, "&lt;b&gt;access_denied") {
		t.Fatalf("the error page does not escape the error:\n%s", body)
	}
	resp, ok := ended(a)
	if !ok || resp.Err == nil || !strings.Contains(resp.Err.Error(), "access_denied") {
		t.Fatalf("the login ended with %+v (ended: %v), want Spotify's error", resp, ok)
	}
	if len(*saved) != 0 {
		t.Fatal("a refused login was saved")
	}
}

func TestCallbackFailures(t *testing.T) {
	cases := []struct {
		name    string
		query   func(a *Authorization) url.Values
		token   func(w http.ResponseWriter, form url.Values)
		saveErr error
		want    string
	}{
		{
			name:  "no code",
			query: func(a *Authorization) url.Values { return url.Values{"state": {a.state}} },
			want:  "no authorization code",
		},
		{
			name:  "token refused",
			query: func(a *Authorization) url.Values { return url.Values{"code": {"c"}, "state": {a.state}} },
			token: func(w http.ResponseWriter, form url.Values) {
				writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Invalid authorization code")
			},
			want: "fail to get token",
		},
		{
			name:    "settings not saved",
			query:   func(a *Authorization) url.Values { return url.Values{"code": {"c"}, "state": {a.state}} },
			saveErr: errors.New("disk full"),
			want:    "disk full",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			token := c.token
			if token == nil {
				token = func(w http.ResponseWriter, form url.Values) { writeToken(w, "access", "refresh") }
			}
			tokenServer(t, token)
			fakeSaveAuth(t, c.saveErr)
			a := NewAuthorization(ownClientID)
			pending(t, a)

			w := callbackRequest(c.query(a))

			resp, ok := ended(a)
			if !ok || resp.Err == nil || !strings.Contains(resp.Err.Error(), c.want) || resp.Client != nil {
				t.Fatalf("the login ended with %+v (ended: %v), want an error with %q", resp, ok, c.want)
			}
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
				t.Fatalf("callback answered %d without the error:\n%s", w.Code, w.Body)
			}
		})
	}
}

func TestNewerLoginCancelsTheOlderOne(t *testing.T) {
	a := NewAuthorization(ownClientID)
	pending(t, a)
	b := NewAuthorization(DefaultClientID)
	pending(t, b)

	if resp, ok := ended(a); !ok || !errors.Is(resp.Err, ErrCanceled) {
		t.Fatalf("the older login ended with %+v (ended: %v), want ErrCanceled", resp, ok)
	}
	if _, ok := ended(b); ok {
		t.Fatal("the newer login ended")
	}

	b.Cancel()
	if resp := b.Wait(); !errors.Is(resp.Err, ErrCanceled) {
		t.Fatalf("Wait() after Cancel = %+v, want ErrCanceled", resp)
	}
	// Wait returns the same again.
	if resp := b.Wait(); !errors.Is(resp.Err, ErrCanceled) {
		t.Fatalf("second Wait() = %+v, want ErrCanceled", resp)
	}
}

func TestCancelAfterTheLoginKeepsItsClient(t *testing.T) {
	tokenServer(t, func(w http.ResponseWriter, form url.Values) { writeToken(w, "access", "refresh") })
	fakeSaveAuth(t, nil)
	a := NewAuthorization(ownClientID)
	pending(t, a)
	callbackRequest(url.Values{"code": {"c"}, "state": {a.state}})

	a.Cancel()
	if resp := a.Wait(); resp.Err != nil || resp.Client == nil {
		t.Fatalf("Wait() = %+v after a late Cancel, want the client", resp)
	}
}

func TestLoginRefreshesWithTheAppOfTheToken(t *testing.T) {
	expired := &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}
	cases := []struct {
		name       string
		clientID   string
		wantClient string
	}{
		{"login with another app", ownClientID, ownClientID},
		{"login of an older spot-go", "", DefaultClientID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forms := tokenServer(t, func(w http.ResponseWriter, form url.Values) {
				writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Refresh token revoked")
			})
			token := *expired
			withSettings(t, &config.Settings{Auth: &config.Auth{Token: &token, Scopes: scopes, ClientID: c.clientID}})

			resp := Login(context.Background())

			if !errors.Is(resp.Err, ErrNeedsAuth) {
				t.Fatalf("Login() = %+v, want ErrNeedsAuth", resp)
			}
			if len(*forms) != 1 {
				t.Fatalf("%d token requests, want 1", len(*forms))
			}
			if got := (*forms)[0]; got.Get("grant_type") != "refresh_token" || got.Get("client_id") != c.wantClient {
				t.Fatalf("refresh request %v, want a refresh with the Client ID %s", got, c.wantClient)
			}
		})
	}
}

func TestLoginEnds(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		code        string
		description string
		ended       bool
	}{
		{"revoked", http.StatusBadRequest, "invalid_grant", "Refresh token revoked", true},
		{"expired", http.StatusBadRequest, "invalid_grant", "Invalid refresh token", true},
		{"app deleted", http.StatusBadRequest, "invalid_client", "Invalid client", true},
		{"server error", http.StatusInternalServerError, "server_error", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokenServer(t, func(w http.ResponseWriter, form url.Values) {
				writeTokenError(w, c.status, c.code, c.description)
			})
			token := &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}
			withSettings(t, &config.Settings{Auth: &config.Auth{Token: token, Scopes: scopes, ClientID: ownClientID}})

			resp := Login(context.Background())

			if got := errors.Is(resp.Err, ErrNeedsAuth); got != c.ended || resp.Err == nil {
				t.Fatalf("Login() = %+v, want ErrNeedsAuth: %v", resp, c.ended)
			}
		})
	}
}

func TestLoginWithAValidTokenDoesNotRefresh(t *testing.T) {
	forms := tokenServer(t, func(w http.ResponseWriter, form url.Values) { writeToken(w, "new", "refresh") })
	token := &oauth2.Token{AccessToken: "valid", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}
	withSettings(t, &config.Settings{Auth: &config.Auth{Token: token, Scopes: scopes, ClientID: ownClientID}})

	resp := Login(context.Background())
	if resp.Err != nil || resp.Client == nil {
		t.Fatalf("Login() = %+v, want a client", resp)
	}
	if len(*forms) != 0 {
		t.Fatal("Login() refreshed a valid token")
	}
}

func TestLoginWithoutTheBuiltInApp(t *testing.T) {
	old := DefaultClientID
	DefaultClientID = ""
	t.Cleanup(func() { DefaultClientID = old })
	token := &oauth2.Token{AccessToken: "valid", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}
	withSettings(t, &config.Settings{Auth: &config.Auth{Token: token, Scopes: scopes}})

	if resp := Login(context.Background()); !errors.Is(resp.Err, ErrNeedsAuth) {
		t.Fatalf("Login() = %+v for a login of a build's own app, in a build without one, want ErrNeedsAuth", resp)
	}
}

func TestLoginCanceledDuringTheTokenRequestIsNotSaved(t *testing.T) {
	var a *Authorization
	exchanging := make(chan struct{})
	tokenServer(t, func(w http.ResponseWriter, form url.Values) {
		close(exchanging)
		// spot-go goes back to the Client ID meanwhile.
		a.Cancel()
		writeToken(w, "access", "refresh")
	})
	saved := fakeSaveAuth(t, nil)
	// After tokenServer, which changes the endpoint.
	a = NewAuthorization(ownClientID)
	pending(t, a)

	w := callbackRequest(url.Values{"code": {"c"}, "state": {a.state}})

	<-exchanging
	if len(*saved) != 0 {
		t.Fatalf("saved %+v, want nothing for a canceled login", *saved)
	}
	if resp := a.Wait(); !errors.Is(resp.Err, ErrCanceled) {
		t.Fatalf("Wait() = %+v, want ErrCanceled", resp)
	}
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "canceled") {
		t.Fatalf("callback answered %d:\n%s", w.Code, w.Body)
	}
}

func TestCanceledLoginDoesNotStart(t *testing.T) {
	a := NewAuthorization(ownClientID)
	a.Cancel()
	if err := a.Start(); !errors.Is(err, ErrCanceled) {
		t.Fatalf("Start() = %v for a canceled login, want ErrCanceled", err)
	}
	callback.mu.Lock()
	defer callback.mu.Unlock()
	if callback.pending == a {
		t.Fatal("a canceled login waits for the callback")
	}
}
