package login

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/browser"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views"
	libspotify "github.com/zmb3/spotify/v2"

	"golang.design/x/clipboard"
)

type (
	// clientIDInput asks for the Client ID of the Spotify app to log in
	// with.
	clientIDInput struct {
		views.WindowSize
		title string
		input textinput.Model
	}
	// clientIDSubmitted is the Client ID typed, or "" for spot-go's own
	// app.
	clientIDSubmitted string

	// browserOpened says the login of authorization started, and the
	// browser opened on it, unless err says why not.
	browserOpened struct {
		authorization *spotify.Authorization
		err           error
	}
	// authorized ends the login of authorization.
	authorized struct {
		authorization *spotify.Authorization
		resp          spotify.ClientResponse
	}
	// accountRefused says Spotify refused the account with the Spotify app
	// spot-go logged in with.
	accountRefused struct {
		err error
	}
)

const (
	clientIDInputWidth = 34
	// messageWidth is the width the paragraphs of the login wrap at.
	messageWidth = 76

	webAPILoginTitle = "Let's log in to Spotify."
	webAPILoginMsg   = `spot-go uses the Spotify Web API through a Spotify app. Spotify only lets
5 accounts use an app in development mode, so spot-go's own app may refuse
yours. To use your own app:`
	// webAPILoginNoDefaultMsg is webAPILoginMsg for a build without an app
	// of its own.
	webAPILoginNoDefaultMsg = "spot-go uses the Spotify Web API through a Spotify app:"
	webAPILoginStepsMsg     = `1. Create an app at %s.
2. Give it the redirect URI %s and the Web API.
3. Paste its Client ID below.`
	webAPILoginDefaultMsg = `Or leave it empty to log in with spot-go's own app.
Press enter to open a browser and log in.`
	webAPIEnvLoginMsg = `Let's log in to Spotify, with the Spotify app of the Client ID
in %s: %s.`
	webAPIOpenBrowserMsg = "Press enter to open a browser and log in."
	webAPIAuthMsg        = "A browser should be opened. Continue the login to Spotify there."
	webAPICopyURLMsg     = `If the browser did not open, press "c" to copy the URL to your clipboard
and paste it in your browser.`
	webAPIBrowserFailedMsg = "spot-go could not open a browser: %s"
	webAPICopyURLOnlyMsg   = `Press "c" to copy the URL to your clipboard, paste it in your browser and
continue the login to Spotify there.`
	webAPIURLCopiedMsg     = "URL copied to clipboard. Paste it in your browser and continue the login."
	webAPIInvalidClientMsg = `If Spotify says "client_id: Invalid", the Client ID is wrong. If it says
redirect_uri is wrong, the app doesn't have the redirect URI
%s.`
	webAPIInvalidIDMsg = `That is not a Client ID.
A Client ID has 32 characters, from 0 to 9 and a to f.`
	webAPIInvalidEnvIDMsg = `%s is not a Client ID.
A Client ID has 32 characters, from 0 to 9 and a to f.`
	webAPINoIDMsg    = "Paste the Client ID of your Spotify app."
	webAPIRefusedMsg = `Spotify refused your account with the Spotify app
%s:
%s`
	webAPIRefusedWhyMsg = `Spotify only lets an app in development mode work for its owner and the
accounts on its user list, up to 5, while the owner has Spotify Premium.`
	// webAPIRefusedDefaultMsg ends with how to log in with another app.
	webAPIRefusedDefaultMsg = `That is spot-go's own app. To use spot-go with your account, create your
own app at %s
and %s.`
	// webAPIRefusedOwnMsg ends with how to log in with another app.
	webAPIRefusedOwnMsg = `Add your account to the app's user list, under User Management in its
settings at %s,
or %s.`
	webAPIRetryMsg = "Press enter to log in again."
)

func newClientIDInput(windowSize views.WindowSize, title, value string) clientIDInput {
	input := textinput.New()
	input.Placeholder = "Client ID"
	input.SetWidth(clientIDInputWidth)
	inputStyles := textinput.DefaultStyles(styles.HasDarkBackground)
	inputStyles.Focused.Prompt = lipgloss.NewStyle()
	inputStyles.Blurred.Prompt = lipgloss.NewStyle()
	inputStyles.Cursor.Color = nil
	inputStyles.Cursor.Blink = false
	input.SetStyles(inputStyles)
	input.SetValue(value)
	input.CursorEnd()
	input.Focus()

	return clientIDInput{
		WindowSize: windowSize,
		title:      title,
		input:      input,
	}
}

func (m clientIDInput) Init() tea.Cmd {
	return nil
}

func (m clientIDInput) CapturesInput() bool {
	return true
}

func (m clientIDInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "enter" {
			value := strings.TrimSpace(m.input.Value())
			return m, func() tea.Msg {
				return clientIDSubmitted(value)
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m clientIDInput) View() tea.View {
	return tea.NewView(lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s\n\n%s", m.title, m.input.View()),
	))
}

// showWebAPILogin asks for the Client ID of the Spotify app to log in with,
// or, when SPOT_GO_CLIENT_ID sets it, to press enter to log in. err, when
// not nil, says why the last login failed.
func (m Model) showWebAPILogin(err error) Model {
	if m.authorization != nil {
		m.authorization.Cancel()
		m.authorization = nil
	}
	m.state = stateWebAPINeedsAuth
	m.activeModel = nil

	if id := spotify.ClientIDFromEnv(); id != "" {
		if !spotify.ValidClientID(id) {
			m.state = stateFatal
			m.msg = fmt.Sprintf(fatalMsg, paragraphs(fmt.Sprintf(webAPIInvalidEnvIDMsg, spotify.ClientIDEnv)))
			return m
		}
		m.msg = paragraphs(errText(err), fmt.Sprintf(webAPIEnvLoginMsg, spotify.ClientIDEnv, id), webAPIOpenBrowserMsg)
		return m
	}

	steps := fmt.Sprintf(webAPILoginStepsMsg, spotify.DashboardURL, spotify.RedirectURI)
	title := paragraphs(errText(err), webAPILoginTitle, webAPILoginMsg, steps, webAPILoginDefaultMsg)
	if spotify.DefaultClientID == "" {
		title = paragraphs(errText(err), webAPILoginTitle, webAPILoginNoDefaultMsg, steps, webAPIOpenBrowserMsg)
	}
	value := m.clientID
	if !m.clientIDChosen {
		if saved := spotify.LoggedInClientID(); saved != spotify.DefaultClientID {
			value = saved
		}
	}
	m.activeModel = newClientIDInput(m.WindowSize, title, value)
	return m
}

// submitClientID logs in with the Spotify app of id, or of spot-go's own app
// when id is "".
func (m Model) submitClientID(id string) (tea.Model, tea.Cmd) {
	if m.state != stateWebAPINeedsAuth {
		return m, nil
	}
	m.clientID, m.clientIDChosen = id, true
	if id == "" {
		id = spotify.DefaultClientID
	}
	if id == "" {
		return m.showWebAPILogin(errors.New(webAPINoIDMsg)), nil
	}
	if !spotify.ValidClientID(id) {
		return m.showWebAPILogin(errors.New(webAPIInvalidIDMsg)), nil
	}
	return m.authorize(id)
}

// authorize starts the login in the browser with the Spotify app of
// clientID.
func (m Model) authorize(clientID string) (tea.Model, tea.Cmd) {
	a := spotify.NewAuthorization(clientID)
	m.authorization = a
	m.activeModel = nil
	m.state = stateWebAPILoggingIn
	m.msg = m.withAuthHints(webAPIAuthMsg, webAPICopyURLMsg)
	return m, func() tea.Msg {
		if err := a.Start(); err != nil {
			return authorized{a, spotify.ClientResponse{Err: err}}
		}
		return browserOpened{a, browser.Open(a.URL)}
	}
}

func (m Model) handleBrowserOpened(msg browserOpened) (tea.Model, tea.Cmd) {
	a := msg.authorization
	if a != m.authorization {
		return m, nil
	}
	if msg.err != nil && m.state == stateWebAPILoggingIn {
		m.msg = m.withAuthHints(fmt.Sprintf(webAPIBrowserFailedMsg, msg.err), webAPICopyURLOnlyMsg)
	}
	return m, func() tea.Msg {
		return authorized{a, a.Wait()}
	}
}

func (m Model) handleAuthorized(msg authorized) (tea.Model, tea.Cmd) {
	if msg.authorization != m.authorization || errors.Is(msg.resp.Err, spotify.ErrCanceled) {
		return m, nil
	}
	m.authorization = nil
	if msg.resp.Err != nil {
		return m.showWebAPILogin(msg.resp.Err), nil
	}
	// The browser is done: spot-go goes on as with a login it saved.
	m.spotifyClient = msg.resp.Client
	m.state = stateInit
	m.msg = m.spinner.View()
	return m, tea.Batch(m.getCurrentUser, m.spinner.Tick)
}

// copyAuthURL copies the URL of the login going on to the clipboard.
func (m Model) copyAuthURL() Model {
	if m.authorization == nil {
		return m
	}
	clipboard.Write(m.ctx, clipboard.FmtText, []byte(m.authorization.URL))
	m.state = stateCopyWebAPIAuthURL
	m.msg = m.withAuthHints(webAPIURLCopiedMsg)
	return m
}

// withAuthHints adds to msgs, the paragraphs shown while the login goes on
// in the browser, what to do when Spotify refuses the app.
func (m Model) withAuthHints(msgs ...string) string {
	back := "Press esc to change the Client ID."
	if spotify.ClientIDFromEnv() != "" {
		back = "Press esc to go back."
	}
	return paragraphs(append(msgs, fmt.Sprintf(webAPIInvalidClientMsg, spotify.RedirectURI)+"\n"+back)...)
}

func (m Model) showAccountRefused(err error) Model {
	m.state = stateWebAPIRefused
	m.activeModel = nil
	m.spotifyClient = nil

	clientID := spotify.LoggedInClientID()
	what := fmt.Sprintf(webAPIRefusedOwnMsg, spotify.DashboardURL, "log in with another app")
	if spotify.ClientIDFromEnv() != "" {
		what = fmt.Sprintf(webAPIRefusedOwnMsg, spotify.DashboardURL, "change "+spotify.ClientIDEnv)
	}
	if clientID == spotify.DefaultClientID {
		what = fmt.Sprintf(webAPIRefusedDefaultMsg, spotify.DashboardURL, "log in with its Client ID")
		if spotify.ClientIDFromEnv() != "" {
			what = fmt.Sprintf(webAPIRefusedDefaultMsg, spotify.DashboardURL, "set "+spotify.ClientIDEnv+" to its Client ID")
		}
	}
	m.msg = paragraphs(fmt.Sprintf(webAPIRefusedMsg, clientID, err), webAPIRefusedWhyMsg, what, webAPIRetryMsg)
	return m
}

// refused reports whether err, from the Web API, says Spotify doesn't let
// the account use the Spotify app spot-go logged in with.
func refused(err error) bool {
	var apiErr libspotify.Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden
}

// errText is the text of err, or "" when it is nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// paragraphs joins the paragraphs that aren't empty, each wrapped at
// messageWidth.
func paragraphs(ps ...string) string {
	var b strings.Builder
	for _, p := range ps {
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(ansi.Wordwrap(p, messageWidth, ""))
	}
	return b.String()
}
