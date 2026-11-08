package login

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/library"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"
	"github.com/spot-go/spot-go/internal/ui/components/secretinput"
	"github.com/spot-go/spot-go/internal/ui/views"
	"github.com/spot-go/spot-go/internal/ui/views/player"
	libspotify "github.com/zmb3/spotify/v2"

	"golang.design/x/clipboard"
)

type (
	Model struct {
		views.WindowSize
		ctx                context.Context
		spotifyClient      *spotify.Client
		spotifyUser        *libspotify.PrivateUser
		playerDevice       *libspotify.PlayerDevice
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
	playerDeviceError struct {
		err error
	}
)

const (
	stateInit state = iota
	stateWebAPINeedsAuth
	stateCopyWebAPIAuthURL
	stateWebAPILoggingIn
	stateWebAPILogged
	stateSoloistDownload
	stateSoloistAPIKey
	stateSoloistLoggingIn
	stateSoloistPairing
	statePlayerReady
	stateFatal

	deviceWaitAttempts = 20

	webAPILoginMsg = `Let's login to spotify. First we need a token to use the Web API.
Press enter to open a browser and login.`
	webAPIAuthMsg = `A browser should be opened. Continue the login to spotify there.

If the browser did not open, press "c" to copy the URL to your clipboard
and past it on your browser manually.`
	webAPIURLCopiedToClipboardMsg = "URL copied to clipboard. Paste it on your browser and continue the login."
	webAPIWelcomeMsg              = `Welcome %s.

Now we are going to configure the spotify player (Soloist).
Press enter to continue`

	fatalMsg = `%s

Press q to quit.`
)

func NewModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return Model{
		ctx:     context.Background(),
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			switch m.state {
			case stateWebAPILogged:
				return m.prepareSoloist()
			case stateWebAPINeedsAuth:
				m.state = stateWebAPILoggingIn
				m.msg = webAPIAuthMsg
				return m, m.webAPIAuth

			default:
				return m, nil
			}
		case "c":
			if m.state == stateWebAPILoggingIn {
				clipboard.Write(m.ctx, clipboard.FmtText, []byte(spotify.AuthURL))
				m.state = stateCopyWebAPIAuthURL
				m.msg = webAPIURLCopiedToClipboardMsg
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
		soloist.Stop()
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
		return m, m.soloistStartupLogin

	case soloist.LoginResponse:
		return m.handleSoloistLogin(msg)

	case soloistPairingNeeded:
		return m.showSoloistPairing(), nil

	case secretinput.Submitted:
		return m.saveSoloistAPIKey(string(msg))

	case playerDeviceError:
		soloist.Stop()
		return m.soloistRetry(msg.err), nil

	case libspotify.PlayerDevice:
		m.state = statePlayerReady
		m.playerDevice = &msg
		mainModel := player.NewModel(
			m.WindowSize,
			m.ctx,
			m.spotifyClient,
			m.spotifyUser,
			m.playerDevice,
			library.NewStore(config.LibraryFile),
		)
		m.player = &mainModel
		return m, m.player.Init()

	case filedownloader.DownloadFinished:
		return m.soloistDownloadFinished()

	case filedownloader.DownloadFailed:
		return m.soloistDownloadFailed(msg.Err)

	default:
		var cmd tea.Cmd
		switch m.state {

		case stateSoloistDownload:
			m.filedownloader, cmd = m.filedownloader.Update(msg)
			m.msg = m.filedownloader.View()
			return m, cmd

		case stateInit, stateSoloistLoggingIn:
			m.spinner, cmd = m.spinner.Update(msg)
			m.msg = m.spinner.View()
			return m, cmd
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m Model) View() tea.View {
	if m.player != nil {
		return m.player.View()
	}

	if m.activeModel != nil {
		return views.FullScreen(m.activeModel.View().Content, m.Height)
	}

	return views.FullScreen(lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		m.msg,
	), m.Height)
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

func (m Model) welcomeMsg() string {
	return fmt.Sprintf(webAPIWelcomeMsg, m.spotifyUser.DisplayName)
}

func (m Model) capturesInput() bool {
	c, ok := m.activeModel.(interface{ CapturesInput() bool })
	return ok && c.CapturesInput()
}

func (m Model) waitForPlayerDevice() tea.Msg {
	for attempt := 0; attempt < deviceWaitAttempts; attempt++ {
		if err := soloist.Err(); err != nil {
			return playerDeviceError{err}
		}

		devices, err := m.spotifyClient.PlayerDevices(m.ctx)
		if err != nil {
			return playerDeviceError{fmt.Errorf("fail to list spotify devices: %w", err)}
		}
		idx := slices.IndexFunc(devices, isPlayerDevice)
		if idx > -1 {
			return devices[idx]
		}
		time.Sleep(1 * time.Second)
	}

	return playerDeviceError{fmt.Errorf(`the device "%s" did not show up in your spotify devices`, soloist.DeviceName())}
}

func isPlayerDevice(d libspotify.PlayerDevice) bool {
	id := soloist.DeviceID()
	return id != "" && string(d.ID) == id
}
