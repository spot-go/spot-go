package login

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spot-go/spot-go/internal/backend"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/ui/components/secretinput"
	libspotify "github.com/zmb3/spotify/v2"
)

func soloistModel() Model {
	m := NewModel(backend.Soloist)
	m.spotifyUser = &libspotify.PrivateUser{User: libspotify.User{DisplayName: "Chico"}}
	return m
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)
	lm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want login.Model", next)
	}
	return lm, cmd
}

func runes(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	return cmd() == tea.Quit()
}

func TestSoloistAPIKeyPromptTakesQ(t *testing.T) {
	m := soloistModel().showSoloistAPIKey(nil)

	m, cmd := update(t, m, runes("q"))
	if isQuit(cmd) {
		t.Fatal(`typing "q" in the API key quit the app`)
	}
	m, cmd = update(t, m, runes("k"))
	if isQuit(cmd) {
		t.Fatal(`typing "k" in the API key quit the app`)
	}

	_, cmd = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not submit the API key")
	}
	if got, ok := cmd().(secretinput.Submitted); !ok || got != "qk" {
		t.Fatalf("enter sent %#v, want Submitted(\"qk\")", got)
	}

	if strings.Contains(m.View().Content, "qk") {
		t.Fatal("the API key is shown in clear text")
	}

	_, cmd = update(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !isQuit(cmd) {
		t.Fatal("ctrl+c did not quit while typing the API key")
	}
}

func TestSoloistInvalidAPIKeyAsksAgain(t *testing.T) {
	m := soloistModel()
	m.state = stateSoloistLoggingIn

	m, _ = update(t, m, soloist.LoginResponse{Err: soloist.ErrInvalidAPIKey})
	if m.state != stateSoloistAPIKey {
		t.Fatalf("state = %v, want the API key prompt", m.state)
	}
	if view := m.View().Content; !strings.Contains(view, soloist.ErrInvalidAPIKey.Error()) || !strings.Contains(view, soloist.APIKeyPage) {
		t.Fatalf("API key prompt does not explain the error and where to get a key:\n%s", view)
	}
}

func TestSoloistPairing(t *testing.T) {
	m := soloistModel()
	m.state = stateSoloistLoggingIn

	m, _ = update(t, m, soloistPairingNeeded{})
	if m.state != stateSoloistPairing {
		t.Fatalf("state = %v, want pairing", m.state)
	}
	if view := m.View().Content; !strings.Contains(view, soloist.DeviceName()) {
		t.Fatalf("pairing view does not name the device:\n%s", view)
	}

	m, cmd := update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) {
		t.Fatal("esc while pairing quit the app")
	}
	if m.state != stateSoloistAPIKey {
		t.Fatalf("state after esc = %v, want the API key prompt", m.state)
	}

	m, _ = update(t, m, soloistPairingNeeded{})
	if m.state != stateSoloistAPIKey {
		t.Fatalf("a late pairing message moved the state to %v", m.state)
	}
	m, _ = update(t, m, soloist.LoginResponse{})
	if m.state != stateSoloistAPIKey {
		t.Fatalf("a late login response moved the state to %v", m.state)
	}
}

func TestSoloistStartupWithoutSetup(t *testing.T) {
	m := soloistModel()

	m, _ = update(t, m, soloist.LoginResponse{Err: errSoloistNotSetUp})
	if m.state != stateWebAPILogged {
		t.Fatalf("state = %v, want the welcome screen", m.state)
	}
	view := m.View().Content
	if !strings.Contains(view, "Welcome Chico") || !strings.Contains(view, "(Soloist)") {
		t.Fatalf("welcome screen does not name the user and Soloist:\n%s", view)
	}
	if strings.Contains(view, "could not start") {
		t.Fatalf("welcome screen shows an error for a first run:\n%s", view)
	}
}

func TestSoloistStartupError(t *testing.T) {
	m := soloistModel()

	m, _ = update(t, m, soloist.LoginResponse{Err: soloist.ErrExpired})
	if m.state != stateWebAPILogged {
		t.Fatalf("state = %v, want the welcome screen", m.state)
	}
	if view := m.View().Content; !strings.Contains(view, soloist.ErrExpired.Error()) {
		t.Fatalf("welcome screen does not show the startup error:\n%s", view)
	}
}

func TestPlaybackErrorsPerPlayer(t *testing.T) {
	if NewModel(backend.Spotifyd).playbackErrors() == nil {
		t.Fatal("spotifyd lost its playback errors")
	}
	if soloistModel().playbackErrors() != nil {
		t.Fatal("Soloist reports spotifyd playback errors")
	}
}

func TestLoginViewsUseTheAltScreen(t *testing.T) {
	cases := map[string]Model{
		"message":       soloistModel(),
		"api key input": soloistModel().showSoloistAPIKey(nil),
	}
	for name, m := range cases {
		if !m.View().AltScreen {
			t.Errorf("%s view is not on the alt screen", name)
		}
	}
}

func TestShortWindowKeepsTheAPIKeyInputVisible(t *testing.T) {
	m := soloistModel()
	m.WindowSizeMsg = tea.WindowSizeMsg{Width: 100, Height: 6}
	m = m.showSoloistAPIKey(nil)

	view := ansi.Strip(m.View().Content)
	if lines := strings.Count(view, "\n") + 1; lines > 6 {
		t.Fatalf("view has %d lines, want at most 6:\n%s", lines, view)
	}
	if !strings.Contains(view, "Soloist API key") {
		t.Fatalf("view does not show the API key input:\n%s", view)
	}
}
