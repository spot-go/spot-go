package login

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/backend"
	"github.com/spot-go/spot-go/internal/program"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/spotifyd"
	"github.com/spot-go/spot-go/internal/ui/components/choice"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"
	"github.com/spot-go/spot-go/internal/ui/components/secretinput"
	"github.com/spot-go/spot-go/internal/ui/views"
	"github.com/spot-go/spot-go/internal/ui/views/player"
	libspotify "github.com/zmb3/spotify/v2"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"golang.design/x/clipboard"
)

type (
	Model struct {
		views.WindowSize
		ctx                context.Context
		backend            backend.Kind
		spotifyClient      *spotify.Client
		spotifyUser        *libspotify.PrivateUser
		playerDevice       *libspotify.PlayerDevice
		spotifydAuthURL    string
		webAPILoginSent    bool
		soloistUpdateTried bool
		msg                string
		state              state
		spinner            spinner.Model
		activeModel        tea.Model
		filedownloader     filedownloader.Model

		player *player.Model
	}

	state int

	fatalError struct {
		err error
	}
	spotifydAuthURLMsg string
	playerDeviceError  struct {
		err error
	}
)

const (
	stateInit state = iota
	stateWebAPINeedsAuth
	stateCopyWebAPIAuthURL
	stateWebAPILoggingIn
	stateWebAPILogged
	stateSpotifydLoggingIn
	stateSpotifydDownload
	stateSpotifydConfig
	stateSpotifydLogged
	stateSpotifydBrowserSelected
	stateSpotifydAppOnLocalNetworkSelected
	stateSoloistDownload
	stateSoloistAPIKey
	stateSoloistLoggingIn
	stateSoloistPairing
	stateFatal

	deviceWaitAttempts = 20

	webAPILoginMsg = `Let's login to spotify. First we need a token to use the Web API.
Press enter to open a browser and login.`
	webAPIAuthMsg = `A browser should be opened. Continue the login to spotify there.

If the browser did not open, press "c" to copy the URL to your clipboard
and past it on your browser manually.`
	webAPIURLCopiedToClipboardMsg = "URL copied to clipboard. Paste it on your browser and continue the login."
	webAPIWelcomeMsg              = `Welcome %s.

Now we are going to configure the spotify player (%s).
Press enter to continue`

	spotifydRetryMsg = "Press enter to try again."
	spotifydReadyMsg = `spotifyd is ready.
Now we need to connect it to your account. Choose one of the options below:`
	spotifydBrowserMsg = `A browser should be opened. Log in to spotify there to connect spotifyd to your account.

If the browser did not open, press "c" to copy the URL to your clipboard
and paste it on your browser manually.

Press esc to go back.`
	spotifydURLCopiedToClipboardMsg = `URL copied to clipboard. Paste it on your browser and continue the login.

Press esc to go back.`
	spotifydUseApp = `Now use a desktop or mobile app connected on your local network
and connect to the device named "%s"

Press esc to go back.`
	fatalMsg = `%s

Press q to quit.`
)

var (
	spotifydConfigChoicesStates = map[string]state{
		"Log in with your browser": stateSpotifydBrowserSelected,

		"Use a desktop or mobile app connected on your local network (may not work with current spotify apps)": stateSpotifydAppOnLocalNetworkSelected,
	}
	spotifydConfigChoices = maps.Keys(spotifydConfigChoicesStates)
)

func init() {
	// Ensure order
	slices.Sort(spotifydConfigChoices)
}

func NewModel(player backend.Kind) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return Model{
		ctx:     context.Background(),
		backend: player,
		msg:     s.View(),
		spinner: s,
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.player != nil {
		return m.player.Update(message)
	}

	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.isSpotifydLoggingIn() {
				spotifyd.Stop()
				return m.showSpotifydChoices(nil), nil
			}
			if m.state == stateSoloistPairing {
				soloist.Stop()
				return m.showSoloistAPIKey(nil), nil
			}
			return m, tea.Quit
		case "q":
			if !m.capturesInput() {
				return m, tea.Quit
			}
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	if m.activeModel != nil {
		var cmd tea.Cmd
		m.activeModel, cmd = m.activeModel.Update(message)
		if cmd != nil {
			return m, cmd
		}
	}

	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			switch m.state {
			case stateWebAPILogged:
				if m.backend == backend.Soloist {
					return m.prepareSoloist()
				}
				return m.prepareSpotifyd()
			case stateWebAPINeedsAuth:
				m.state = stateWebAPILoggingIn
				m.msg = webAPIAuthMsg
				return m, m.webAPIAuth

			default:
				return m, nil
			}
		case "c":
			switch m.state {
			case stateWebAPILoggingIn:
				clipboard.Write(clipboard.FmtText, []byte(spotify.AuthURL))
				m.state = stateCopyWebAPIAuthURL
				m.msg = webAPIURLCopiedToClipboardMsg
			case stateSpotifydBrowserSelected:
				if m.spotifydAuthURL != "" {
					clipboard.Write(clipboard.FmtText, []byte(m.spotifydAuthURL))
					m.msg = spotifydURLCopiedToClipboardMsg
				}
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		if m.state == stateInit && !m.webAPILoginSent {
			m.webAPILoginSent = true
			return m, m.webAPILogin
		}

		return m, nil

	case fatalError:
		backend.Stop()
		m.state = stateFatal
		m.activeModel = nil
		m.msg = fmt.Sprintf(fatalMsg, msg.err)
		return m, nil

	case spotify.ClientResponse:
		if errors.Is(msg.Err, spotify.ErrNeedsAuth) {
			m.msg = webAPILoginMsg
			m.state = stateWebAPINeedsAuth
			return m, nil
		}

		if msg.Err != nil {
			return m.Update(fatalError{msg.Err})
		}
		m.spotifyClient = msg.Client
		return m, m.getCurrentUser

	case *libspotify.PrivateUser:
		m.spotifyUser = msg
		if m.backend == backend.Soloist {
			return m, m.soloistStartupLogin
		}
		return m, m.spotifydLogin

	case spotifyd.LoginResponse:
		if errors.Is(msg.Err, spotifyd.ErrStopped) || !m.isWaitingSpotifydLogin() {
			return m, nil
		}

		if msg.Err != nil {
			if m.state <= stateWebAPILoggingIn {
				m.state = stateWebAPILogged
				m.msg = m.welcomeMsg()
				if !errors.Is(msg.Err, spotifyd.ErrCredentialsNotFound) {
					m.msg = fmt.Sprintf("%s\n\nspotifyd could not log in with the saved credentials:\n%s", m.msg, msg.Err)
				}
				return m, nil
			}
			return m.showSpotifydChoices(msg.Err), nil
		}

		m.msg = m.spinner.View()
		m.activeModel = nil
		m.state = stateSpotifydLoggingIn
		return m, tea.Batch(m.waitForPlayerDevice, m.spinner.Tick)

	case spotifyd.AuthenticateResponse:
		if errors.Is(msg.Err, spotifyd.ErrStopped) || m.state != stateSpotifydBrowserSelected {
			return m, nil
		}

		if msg.Err != nil {
			return m.showSpotifydChoices(msg.Err), nil
		}

		m.msg = m.spinner.View()
		m.state = stateSpotifydLoggingIn
		return m, tea.Batch(m.spotifydLogin, m.spinner.Tick)

	case spotifydAuthURLMsg:
		if m.state == stateSpotifydBrowserSelected {
			m.spotifydAuthURL = string(msg)
		}
		return m, nil

	case soloist.LoginResponse:
		return m.handleSoloistLogin(msg)

	case soloistPairingNeeded:
		return m.showSoloistPairing(), nil

	case secretinput.Submitted:
		return m.saveSoloistAPIKey(string(msg))

	case playerDeviceError:
		if m.backend == backend.Soloist {
			soloist.Stop()
			return m.soloistRetry(msg.err), nil
		}
		spotifyd.Stop()
		return m.showSpotifydChoices(msg.err), nil

	case libspotify.PlayerDevice:
		m.state = stateSpotifydLogged
		m.playerDevice = &msg
		mainModel := player.NewModel(
			m.WindowSize,
			m.ctx,
			m.spotifyClient,
			m.spotifyUser,
			m.playerDevice,
			m.playbackErrors(),
		)
		m.player = &mainModel
		return m, m.player.Init()

	case filedownloader.DownloadFinished:
		if m.backend == backend.Soloist {
			return m.soloistDownloadFinished()
		}
		if err := spotifyd.CheckBinary(); err != nil {
			m.state = stateWebAPILogged
			m.msg = fmt.Sprintf("%s\n\n%s", err, spotifydRetryMsg)
			return m, nil
		}
		return m.showSpotifydChoices(nil), nil

	case filedownloader.DownloadFailed:
		if m.backend == backend.Soloist {
			return m.soloistDownloadFailed(msg.Err)
		}
		m.state = stateWebAPILogged
		m.msg = fmt.Sprintf("Fail to download spotifyd: %s\n\n%s", msg.Err, spotifydRetryMsg)
		return m, nil

	case choice.Selected:
		if m.state != stateSpotifydConfig {
			return m, nil
		}
		m.state = spotifydConfigChoicesStates[string(msg)]
		m.activeModel = nil
		m.spotifydAuthURL = ""
		switch m.state {
		case stateSpotifydBrowserSelected:
			m.msg = spotifydBrowserMsg
			return m, m.spotifydAuthenticate
		default:
			m.msg = fmt.Sprintf(spotifydUseApp, spotifyd.DeviceName())
			return m, m.spotifydLoginDiscovery
		}

	default:
		var cmd tea.Cmd
		switch m.state {

		case stateSpotifydDownload, stateSoloistDownload:
			m.filedownloader, cmd = m.filedownloader.Update(msg)
			m.msg = m.filedownloader.View()
			return m, cmd

		case stateInit, stateSpotifydLoggingIn, stateSoloistLoggingIn:
			m.spinner, cmd = m.spinner.Update(msg)
			m.msg = m.spinner.View()
			return m, cmd
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.player != nil {
		return m.player.View()
	}

	if m.activeModel != nil {
		return m.activeModel.View()
	}

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		m.msg,
	)
}

func (m Model) isSpotifydLoggingIn() bool {
	return m.state == stateSpotifydBrowserSelected ||
		m.state == stateSpotifydAppOnLocalNetworkSelected
}

func (m Model) isWaitingSpotifydLogin() bool {
	return m.state <= stateWebAPILoggingIn ||
		m.state == stateSpotifydLoggingIn ||
		m.state == stateSpotifydAppOnLocalNetworkSelected
}

func (m Model) prepareSpotifyd() (tea.Model, tea.Cmd) {
	err := spotifyd.CheckBinary()
	if err == nil {
		return m, func() tea.Msg {
			return filedownloader.DownloadFinished(true)
		}
	}

	url, ok := spotifyd.DownloadURL()
	if !ok {
		m.msg = fmt.Sprintf("%s\n\n%s", err, spotifydRetryMsg)
		return m, nil
	}

	m.filedownloader = filedownloader.NewModel(url, spotifyd.ConfigFolder, "spotifyd", spotifyd.InstallArchive)
	m.msg = m.filedownloader.View()
	m.state = stateSpotifydDownload
	return m, m.filedownloader.Init()
}

func (m Model) showSpotifydChoices(err error) Model {
	title := spotifydReadyMsg
	if err != nil {
		title = fmt.Sprintf("spotifyd login failed: %s\n\n%s", err, title)
	}

	m.state = stateSpotifydConfig
	m.spotifydAuthURL = ""
	m.activeModel = choice.NewModel(m.WindowSize, title, spotifydConfigChoices)
	return m
}

func (m Model) webAPILogin() tea.Msg {
	return spotify.Login(m.ctx)
}

func (m Model) webAPIAuth() tea.Msg {
	return spotify.Authorize()
}

func (m Model) getCurrentUser() tea.Msg {
	u, err := m.spotifyClient.CurrentUser(m.ctx)
	if err != nil {
		return fatalError{fmt.Errorf("fail to get the current spotify user: %w", err)}
	}

	return u
}

func (m Model) spotifydLogin() tea.Msg {
	return spotifyd.LoginWithCredentials()
}

func (m Model) spotifydLoginDiscovery() tea.Msg {
	return spotifyd.LoginDiscovery()
}

func (m Model) spotifydAuthenticate() tea.Msg {
	return spotifyd.Authenticate(func(url string) {
		go program.Instance.Send(spotifydAuthURLMsg(url))
	})
}

func (m Model) welcomeMsg() string {
	return fmt.Sprintf(webAPIWelcomeMsg, m.spotifyUser.DisplayName, m.backend.Name())
}

func (m Model) capturesInput() bool {
	c, ok := m.activeModel.(interface{ CapturesInput() bool })
	return ok && c.CapturesInput()
}

func (m Model) waitForPlayerDevice() tea.Msg {
	for attempt := 0; attempt < deviceWaitAttempts; attempt++ {
		if err := m.playerErr(); err != nil {
			return playerDeviceError{err}
		}

		devices, err := m.spotifyClient.PlayerDevices(m.ctx)
		if err != nil {
			return playerDeviceError{fmt.Errorf("fail to list spotify devices: %w", err)}
		}
		idx := slices.IndexFunc(devices, m.isPlayerDevice)
		if idx > -1 {
			return devices[idx]
		}
		time.Sleep(1 * time.Second)
	}

	return playerDeviceError{fmt.Errorf(`the device "%s" did not show up in your spotify devices`, m.playerDeviceName())}
}

func (m Model) playerErr() error {
	if m.backend == backend.Soloist {
		return soloist.Err()
	}
	return spotifyd.Err()
}

func (m Model) isPlayerDevice(d libspotify.PlayerDevice) bool {
	if m.backend == backend.Soloist {
		id := soloist.DeviceID()
		return id != "" && string(d.ID) == id
	}
	return d.Name == spotifyd.DeviceName()
}

func (m Model) playerDeviceName() string {
	if m.backend == backend.Soloist {
		return soloist.DeviceName()
	}
	return spotifyd.DeviceName()
}

func (m Model) playbackErrors() <-chan error {
	if m.backend == backend.Soloist {
		return nil
	}
	return spotifyd.PlaybackErrors()
}
