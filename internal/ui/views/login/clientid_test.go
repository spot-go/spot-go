package login

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/spotify"
	"golang.org/x/oauth2"
)

const ownClientID = "0123456789abcdef0123456789abcdef"

// withSettings makes s the settings spot-go loaded, for the test.
func withSettings(t *testing.T, s *config.Settings) {
	t.Helper()
	old := config.AppSettings
	config.AppSettings = s
	t.Cleanup(func() { config.AppSettings = old })
}

// loggedIn is the settings of a login with the app of clientID, or of a
// login an older spot-go saved when clientID is "".
func loggedIn(clientID string) *config.Settings {
	token := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	return &config.Settings{Auth: &config.Auth{Token: token, ClientID: clientID}}
}

// needsLogin is the login view once spot-go found no login it can use.
func needsLogin(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil)
	m.WindowSizeMsg = tea.WindowSizeMsg{Width: 100, Height: 40}
	m, _ = update(t, m, spotify.ClientResponse{Err: spotify.ErrNeedsAuth})
	t.Cleanup(func() {
		if m.authorization != nil {
			m.authorization.Cancel()
		}
	})
	return m
}

// view is the text the view shows, with the lines its paragraphs wrap at
// joined.
func view(m Model) string {
	return strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
}

func clientIDValue(t *testing.T, m Model) string {
	t.Helper()
	in, ok := m.activeModel.(clientIDInput)
	if !ok {
		t.Fatalf("the view shows %T, want the Client ID input", m.activeModel)
	}
	return in.input.Value()
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = update(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
		if isQuit(cmd) {
			t.Fatalf("typing %q quit the app", r)
		}
	}
	return m
}

// submit presses enter in the Client ID input, and gives the view what it
// submits.
func submit(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not submit the Client ID")
	}
	msg, ok := cmd().(clientIDSubmitted)
	if !ok {
		t.Fatalf("enter sent %T, want clientIDSubmitted", msg)
	}
	m, _ = update(t, m, msg)
	return m
}

func TestLoginAsksForTheClientID(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := needsLogin(t)

	if m.state != stateWebAPINeedsAuth {
		t.Fatalf("state = %v, want the login", m.state)
	}
	v := view(m)
	for _, want := range []string{spotify.DashboardURL, spotify.RedirectURI, "Web API", "Client ID", "leave it empty"} {
		if !strings.Contains(v, want) {
			t.Errorf("the login does not say %q:\n%s", want, v)
		}
	}
	if got := clientIDValue(t, m); got != "" {
		t.Fatalf("the Client ID input starts with %q, want it empty", got)
	}

	m = typeText(t, m, "qkc")
	if got := clientIDValue(t, m); got != "qkc" {
		t.Fatalf("typed %q, want qkc", got)
	}
	if !strings.Contains(view(m), "qkc") {
		t.Fatal("the Client ID typed is not shown")
	}
}

func TestClientIDInputStartsWithTheSavedOne(t *testing.T) {
	withSettings(t, loggedIn(ownClientID))
	if got := clientIDValue(t, needsLogin(t)); got != ownClientID {
		t.Fatalf("the Client ID input starts with %q, want the saved %q", got, ownClientID)
	}

	withSettings(t, loggedIn(""))
	if got := clientIDValue(t, needsLogin(t)); got != "" {
		t.Fatalf("the Client ID input starts with %q after a login with spot-go's own app, want it empty", got)
	}
}

func TestEmptyClientIDLogsInWithSpotGosApp(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, needsLogin(t))

	if m.state != stateWebAPILoggingIn || m.authorization == nil {
		t.Fatalf("state = %v, authorization = %v, want the login in the browser", m.state, m.authorization)
	}
	if m.authorization.ClientID != spotify.DefaultClientID {
		t.Fatalf("logs in with %q, want spot-go's own app", m.authorization.ClientID)
	}
	v := view(m)
	for _, want := range []string{"INVALID_CLIENT", spotify.RedirectURI, `"c"`, "esc to change the Client ID"} {
		if !strings.Contains(v, want) {
			t.Errorf("the login in the browser does not say %q:\n%s", want, v)
		}
	}
}

func TestOwnClientIDLogsInWithIt(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := needsLogin(t)
	m = submit(t, typeText(t, m, " "+ownClientID+" "))

	if m.state != stateWebAPILoggingIn || m.authorization == nil || m.authorization.ClientID != ownClientID {
		t.Fatalf("state = %v, authorization = %+v, want a login with %s", m.state, m.authorization, ownClientID)
	}
	if !strings.Contains(m.authorization.URL, "client_id="+ownClientID) {
		t.Fatalf("login URL %s is not of the app typed", m.authorization.URL)
	}
}

func TestInvalidClientIDAsksAgain(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, typeText(t, needsLogin(t), "my secret"))

	if m.state != stateWebAPINeedsAuth || m.authorization != nil {
		t.Fatalf("state = %v, authorization = %v, want the Client ID input again", m.state, m.authorization)
	}
	if got := clientIDValue(t, m); got != "my secret" {
		t.Fatalf("the input has %q, want what was typed", got)
	}
	if v := view(m); !strings.Contains(v, "32 characters") {
		t.Fatalf("the input does not say why the Client ID is wrong:\n%s", v)
	}
}

func TestEscWhileLoggingInGoesBackToTheClientID(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, typeText(t, needsLogin(t), ownClientID))
	a := m.authorization

	m, cmd := update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) {
		t.Fatal("esc while logging in quit the app")
	}
	if m.state != stateWebAPINeedsAuth || m.authorization != nil {
		t.Fatalf("state = %v after esc, want the Client ID input", m.state)
	}
	if got := clientIDValue(t, m); got != ownClientID {
		t.Fatalf("the input has %q after esc, want the Client ID typed", got)
	}
	if resp := a.Wait(); !errors.Is(resp.Err, spotify.ErrCanceled) {
		t.Fatalf("the login ended with %+v after esc, want ErrCanceled", resp)
	}

	// The end of the canceled login comes late.
	m, _ = update(t, m, authorized{a, a.Wait()})
	if m.state != stateWebAPINeedsAuth {
		t.Fatalf("the canceled login moved the state to %v", m.state)
	}

	_, cmd = update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !isQuit(cmd) {
		t.Fatal("esc in the Client ID input did not quit")
	}
}

func TestFailedLoginShowsWhy(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, typeText(t, needsLogin(t), ownClientID))

	m, _ = update(t, m, authorized{m.authorization, spotify.ClientResponse{Err: errors.New("Spotify did not log you in: access_denied")}})

	if m.state != stateWebAPINeedsAuth {
		t.Fatalf("state = %v, want the Client ID input", m.state)
	}
	if v := view(m); !strings.Contains(v, "access_denied") {
		t.Fatalf("the input does not say why the login failed:\n%s", v)
	}
	if got := clientIDValue(t, m); got != ownClientID {
		t.Fatalf("the input has %q, want the Client ID typed", got)
	}
}

func TestEndOfAnOlderLoginIsIgnored(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, needsLogin(t))
	older := spotify.NewAuthorization(ownClientID)

	m, cmd := update(t, m, authorized{older, spotify.ClientResponse{Client: spotify.NewClient(http.DefaultClient)}})
	if m.state != stateWebAPILoggingIn || m.spotifyClient != nil || cmd != nil {
		t.Fatalf("the end of an older login moved the state to %v", m.state)
	}
	m, cmd = update(t, m, browserOpened{older, nil})
	if cmd != nil {
		t.Fatal("the browser of an older login waits for it")
	}
}

func TestBrowserFailureSaysHowToCopyTheURL(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := submit(t, needsLogin(t))

	m, cmd := update(t, m, browserOpened{m.authorization, errors.New("xdg-open: not found")})
	if cmd == nil {
		t.Fatal("the login does not wait after the browser failed to open")
	}
	v := view(m)
	if !strings.Contains(v, "xdg-open: not found") || !strings.Contains(v, `"c"`) {
		t.Fatalf("the view does not say the browser failed and how to copy the URL:\n%s", v)
	}
}

func TestLoginWithTheClientIDOfTheEnvironment(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	t.Setenv(spotify.ClientIDEnv, ownClientID)
	m := needsLogin(t)

	if m.activeModel != nil {
		t.Fatalf("the login shows %T with %s set, want no input", m.activeModel, spotify.ClientIDEnv)
	}
	v := view(m)
	if !strings.Contains(v, spotify.ClientIDEnv) || !strings.Contains(v, ownClientID) {
		t.Fatalf("the login does not name the variable and its Client ID:\n%s", v)
	}

	m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != stateWebAPILoggingIn || m.authorization == nil || m.authorization.ClientID != ownClientID {
		t.Fatalf("state = %v, authorization = %+v after enter, want a login with %s", m.state, m.authorization, ownClientID)
	}
	if v := view(m); !strings.Contains(v, "esc to go back") {
		t.Fatalf("the login in the browser does not say how to go back:\n%s", v)
	}

	m, cmd := update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) || m.state != stateWebAPINeedsAuth || m.activeModel != nil {
		t.Fatalf("esc went to state %v (quit: %v), want the login again", m.state, isQuit(cmd))
	}
}

func TestInvalidClientIDInTheEnvironment(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	t.Setenv(spotify.ClientIDEnv, "my-app")
	m := needsLogin(t)

	if m.state != stateFatal {
		t.Fatalf("state = %v, want the error", m.state)
	}
	if v := view(m); !strings.Contains(v, spotify.ClientIDEnv) || !strings.Contains(v, "32 characters") {
		t.Fatalf("the error does not name the variable and why:\n%s", v)
	}
}

func TestClientIDOfTheEnvironmentNeedsALoginWithItsApp(t *testing.T) {
	m := NewModel(nil)
	cases := []struct {
		name     string
		saved    string
		env      string
		needsNew bool
	}{
		{"no variable", ownClientID, "", false},
		{"same app", ownClientID, ownClientID, false},
		{"another app", ownClientID, spotify.DefaultClientID, true},
		{"login of an older spot-go, same app", "", spotify.DefaultClientID, false},
		{"login of an older spot-go, another app", "", ownClientID, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := loggedIn(c.saved)
			s.Auth.Scopes = loginScopes(t)
			withSettings(t, s)
			t.Setenv(spotify.ClientIDEnv, c.env)

			resp, ok := m.webAPILogin().(spotify.ClientResponse)
			if !ok {
				t.Fatal("webAPILogin() did not log in")
			}
			if got := errors.Is(resp.Err, spotify.ErrNeedsAuth); got != c.needsNew {
				t.Fatalf("webAPILogin() = %+v, want ErrNeedsAuth: %v", resp, c.needsNew)
			}
		})
	}
}

// loginScopes returns the scopes spot-go logs in with.
func loginScopes(t *testing.T) []string {
	t.Helper()
	u, err := url.Parse(spotify.NewAuthorization(ownClientID).URL)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(u.Query().Get("scope"))
}

// refusingAPI answers every Web API request with a 403, as Spotify does for
// an account not on the user list of an app in development mode.
type refusingAPI struct{}

func (refusingAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"status":403,"message":"Check settings on developer.spotify.com/dashboard, the user may not be registered."}}`)),
		Request:    r,
	}, nil
}

func TestRefusedAccountLetsYouLogInAgain(t *testing.T) {
	cases := []struct {
		name  string
		saved string
		env   string
		want  []string
	}{
		{"spot-go's own app", "", "", []string{"spot-go's own app", spotify.DashboardURL, "log in with its Client ID"}},
		{"own app", ownClientID, "", []string{ownClientID, "User Management", "log in with another app"}},
		{"own app of the environment", ownClientID, ownClientID, []string{"User Management", "change " + spotify.ClientIDEnv}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withSettings(t, loggedIn(c.saved))
			t.Setenv(spotify.ClientIDEnv, c.env)
			m := soloistModel()
			m.spotifyUser = nil
			m.spotifyClient = spotify.NewClient(&http.Client{Transport: refusingAPI{}})

			msg := m.getCurrentUser()
			if _, ok := msg.(accountRefused); !ok {
				t.Fatalf("getCurrentUser() = %#v for a 403, want accountRefused", msg)
			}
			m, _ = update(t, m, msg)
			if m.state != stateWebAPIRefused || m.spotifyClient != nil {
				t.Fatalf("state = %v, want the refused account", m.state)
			}
			v := view(m)
			for _, want := range append([]string{"the user may not be registered", "user list", webAPIRetryMsg}, c.want...) {
				if !strings.Contains(v, want) {
					t.Errorf("the refused account does not say %q:\n%s", want, v)
				}
			}

			m, _ = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.state != stateWebAPINeedsAuth {
				t.Fatalf("state after enter = %v, want the login", m.state)
			}
		})
	}
}

func TestOtherCurrentUserErrorsAreFatal(t *testing.T) {
	if refused(errors.New("connection refused")) {
		t.Fatal("refused() = true for a network error")
	}
}

func TestShortWindowKeepsTheClientIDInputVisible(t *testing.T) {
	withSettings(t, &config.Settings{Auth: &config.Auth{}})
	m := NewModel(nil)
	m.WindowSizeMsg = tea.WindowSizeMsg{Width: 100, Height: 6}
	m, _ = update(t, m, spotify.ClientResponse{Err: spotify.ErrNeedsAuth})

	v := ansi.Strip(m.View().Content)
	if lines := strings.Count(v, "\n") + 1; lines > 6 {
		t.Fatalf("view has %d lines, want at most 6:\n%s", lines, v)
	}
	if !strings.Contains(v, "Client ID") {
		t.Fatalf("view does not show the Client ID input:\n%s", v)
	}
}

func TestWithoutABuiltInAppTheClientIDIsNeeded(t *testing.T) {
	old := spotify.DefaultClientID
	spotify.DefaultClientID = ""
	t.Cleanup(func() { spotify.DefaultClientID = old })
	withSettings(t, &config.Settings{Auth: &config.Auth{}})

	m := needsLogin(t)
	if v := view(m); strings.Contains(v, "leave it empty") || strings.Contains(v, "spot-go's own app") {
		t.Fatalf("the login offers spot-go's own app in a build without one:\n%s", v)
	}
	m = submit(t, m)
	if m.state != stateWebAPINeedsAuth || m.authorization != nil {
		t.Fatalf("state = %v with no Client ID in a build without its own app, want the input again", m.state)
	}
	if v := view(m); !strings.Contains(v, "Paste the Client ID") {
		t.Fatalf("the input does not ask for the Client ID:\n%s", v)
	}
}
