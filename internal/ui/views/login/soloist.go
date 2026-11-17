package login

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/program"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"
	"github.com/spot-go/spot-go/internal/ui/components/secretinput"
)

type soloistPairingNeeded struct{}

const (
	soloistRetryMsg  = "Press enter to try again."
	soloistAPIKeyMsg = `spot-go plays the audio with Soloist, Spotify's player for headless devices.
Soloist needs an API key created by a Spotify Premium account:

1. Open %s and log in.
2. Accept the terms if asked and generate an API key.
3. Paste the key below and press enter.

The key is saved only on this computer.`
	soloistPairingMsg = `Soloist is waiting to be connected to your account.

Open Spotify on a phone or computer on the same network,
open the list of devices and select "%s".

Press esc to change the Soloist API key.`
)

var errSoloistNotSetUp = errors.New("Soloist is not set up")

// soloistStartupLogin uses the Soloist an earlier run of spot-go left
// playing, or else starts Soloist, when it is set up. A Soloist left playing
// keeps its build: a newer one, when the build gets old, waits for the next
// time spot-go starts Soloist, so what plays isn't cut.
func (m Model) soloistStartupLogin() tea.Msg {
	if !soloist.HasAPIKey() || (soloist.NeedsDownload() && !soloist.LeftPlaying()) {
		return soloist.LoginResponse{Err: errSoloistNotSetUp}
	}
	return m.soloistResume()
}

func (m Model) soloistResume() tea.Msg {
	return soloist.Resume(pairingNeeded)
}

func (m Model) soloistLogin() tea.Msg {
	return soloist.Login(pairingNeeded)
}

func pairingNeeded() {
	go program.Instance.Send(soloistPairingNeeded{})
}

// KeepPlaying reports whether Soloist keeps playing when spot-go quits
// before the player shows: only when it is one an earlier run of spot-go
// left playing, which keeps playing as it was found.
func (m Model) KeepPlaying() bool {
	return soloist.Adopted()
}

func (m Model) isWaitingSoloistLogin() bool {
	return m.state <= stateWebAPILoggingIn ||
		m.state == stateSoloistLoggingIn ||
		m.state == stateSoloistPairing
}

func (m Model) handleSoloistLogin(msg soloist.LoginResponse) (tea.Model, tea.Cmd) {
	if errors.Is(msg.Err, soloist.ErrStopped) || !m.isWaitingSoloistLogin() {
		return m, nil
	}

	switch {
	case msg.Err == nil:
		m.msg = m.spinner.View()
		m.activeModel = nil
		m.state = stateSoloistLoggingIn
		return m, tea.Batch(m.waitForPlayerDevice, m.spinner.Tick)

	case errors.Is(msg.Err, soloist.ErrInvalidAPIKey), errors.Is(msg.Err, soloist.ErrNoAPIKey):
		return m.showSoloistAPIKey(msg.Err), nil

	case m.state <= stateWebAPILoggingIn:
		m.state = stateWebAPILogged
		m.msg = m.welcomeMsg()
		if !errors.Is(msg.Err, errSoloistNotSetUp) {
			m.msg = fmt.Sprintf("%s\n\nSoloist could not start:\n%s", m.msg, msg.Err)
		}
		return m, nil

	default:
		return m.soloistRetry(msg.Err), nil
	}
}

func (m Model) prepareSoloist() (tea.Model, tea.Cmd) {
	if _, err := soloist.CheckBinary(); err != nil || (soloist.NeedsDownload() && !m.soloistUpdateTried) {
		return m.downloadSoloist()
	}
	if !soloist.HasAPIKey() {
		return m.showSoloistAPIKey(nil), nil
	}
	return m.startSoloist()
}

func (m Model) downloadSoloist() (tea.Model, tea.Cmd) {
	url, ok := soloist.DownloadURL()
	if !ok {
		return m.soloistRetry(errors.New("Soloist has no build for this platform")), nil
	}

	m.activeModel = nil
	m.filedownloader = filedownloader.NewModel(url, soloist.Folder, "Soloist", soloist.InstallArchive)
	m.msg = m.filedownloader.View()
	m.state = stateSoloistDownload
	return m, m.filedownloader.Init()
}

func (m Model) soloistDownloadFinished() (tea.Model, tea.Cmd) {
	m.soloistUpdateTried = true
	if _, err := soloist.CheckBinary(); err != nil {
		return m.soloistRetry(err), nil
	}
	return m.prepareSoloist()
}

func (m Model) soloistDownloadFailed(err error) (tea.Model, tea.Cmd) {
	m.soloistUpdateTried = true
	if _, checkErr := soloist.CheckBinary(); checkErr == nil {
		if !soloist.HasAPIKey() {
			return m.showSoloistAPIKey(nil), nil
		}
		// The build in place still works, and so does a Soloist an earlier
		// run of spot-go left playing with it.
		return m.startSoloistWith(m.soloistResume)
	}

	m.state = stateWebAPILogged
	m.msg = fmt.Sprintf("Fail to download Soloist: %s\n\nDownloads: %s\n\n%s", err, soloist.DownloadsPage, soloistRetryMsg)
	return m, nil
}

func (m Model) startSoloist() (tea.Model, tea.Cmd) {
	return m.startSoloistWith(m.soloistLogin)
}

func (m Model) startSoloistWith(login tea.Cmd) (tea.Model, tea.Cmd) {
	m.state = stateSoloistLoggingIn
	m.activeModel = nil
	m.msg = m.spinner.View()
	return m, tea.Batch(login, m.spinner.Tick)
}

func (m Model) showSoloistAPIKey(err error) Model {
	title := fmt.Sprintf(soloistAPIKeyMsg, soloist.APIKeyPage)
	if err != nil && !errors.Is(err, soloist.ErrNoAPIKey) {
		title = fmt.Sprintf("%s\n\n%s", err, title)
	}

	m.state = stateSoloistAPIKey
	m.activeModel = secretinput.NewModel(m.WindowSize, title, "Soloist API key")
	return m
}

func (m Model) saveSoloistAPIKey(key string) (tea.Model, tea.Cmd) {
	if m.state != stateSoloistAPIKey {
		return m, nil
	}
	if err := soloist.SaveAPIKey(key); err != nil {
		return m.showSoloistAPIKey(err), nil
	}
	return m.startSoloist()
}

func (m Model) showSoloistPairing() Model {
	if !m.isWaitingSoloistLogin() {
		return m
	}

	m.state = stateSoloistPairing
	m.activeModel = nil
	m.msg = fmt.Sprintf(soloistPairingMsg, soloist.DeviceName())
	return m
}

func (m Model) soloistRetry(err error) Model {
	m.state = stateWebAPILogged
	m.activeModel = nil
	m.msg = fmt.Sprintf("Soloist could not start:\n%s\n\n%s", err, soloistRetryMsg)
	return m
}
