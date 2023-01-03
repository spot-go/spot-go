package login

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/spotifyd"
	"github.com/spot-go/spot-go/internal/ui/components/choice"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"
	"github.com/spot-go/spot-go/internal/ui/views"
	"github.com/spot-go/spot-go/internal/ui/views/player"
	spotifydview "github.com/spot-go/spot-go/internal/ui/views/spotifyd"
	libspotify "github.com/zmb3/spotify/v2"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"golang.design/x/clipboard"
)

type (
	Model struct {
		views.WindowSize
		ctx            context.Context
		spotifyClient  *spotify.Client
		spotifyUser    *libspotify.PrivateUser
		spotifydPlayer *libspotify.PlayerDevice
		msg            string
		state          state
		spinner        spinner.Model
		activeModel    tea.Model
		filedownloader filedownloader.Model

		player *player.Model
	}

	state int
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
	stateSpotifydUserPasswordSelected
	stateSpotifydAppOnLocalNetworkSelected

	webAPILoginMsg = `Let's login to spotify. First we need a token to use the Web API.
Press enter to open a browser and login.`
	webAPIAuthMsg = `A browser should be opened. Continue the login to spotify there.

If the browser did not open, press "c" to copy the URL to your clipboard
and past it on your browser manually.`
	webAPIURLCopiedToClipboardMsg = "URL copied to clipboard. Paste it on your browser and continue the login."
	webAPIWelcomeMsg              = `Welcome %s.

Now we are going to configure the spotify player (spotifyd).
Press enter to continue`

	spotifydDownloadMsg = `spotifyd downloaded.
Now we need to connect it to your account. Choose one of the options below:`

	spotifydUseApp = `Now use a desktop or mobile app connected on your local network
and connect to the device named "%s"`
)

var (
	spotifydConfigChoicesStates = map[string]state{
		"Type username and password": stateSpotifydUserPasswordSelected,

		"Use a desktop or mobile app connected on your local network": stateSpotifydAppOnLocalNetworkSelected,
	}
	spotifydConfigChoices = maps.Keys(spotifydConfigChoicesStates)
)

func init() {
	// Ensure order
	slices.Sort(spotifydConfigChoices)
}

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
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
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
				if spotifyd.ShouldDownloadBinary() {
					m.filedownloader = filedownloader.NewModel(
						"https://github.com/Spotifyd/spotifyd/releases/download/v0.3.4/spotifyd-linux-full.tar.gz",
						spotifyd.ConfigFolder,
					)
					m.msg = m.filedownloader.View()
					m.state = stateSpotifydDownload
					return m, m.filedownloader.Init()
				}

				return m, func() tea.Msg {
					return filedownloader.DownloadFinished(true)
				}
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
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		if m.state == stateInit {
			return m, m.webAPILogin
		}

		return m, nil

	case spotify.ClientResponse:
		if errors.Is(msg.Err, spotify.ErrNeedsAuth) {
			m.msg = webAPILoginMsg
			m.state = stateWebAPINeedsAuth
			return m, nil
		}

		if msg.Err != nil {
			fmt.Println(msg.Err)
			os.Exit(1)
		}
		m.spotifyClient = msg.Client
		return m, m.getCurrentUser

	case *libspotify.PrivateUser:
		m.spotifyUser = msg
		return m, m.spotifydLogin

	case spotifyd.LoginResponse:
		if msg.Err != nil {
			if m.state <= stateWebAPILoggingIn {
				m.state = stateWebAPILogged
				m.msg = fmt.Sprintf(webAPIWelcomeMsg, m.spotifyUser.DisplayName)
				return m, nil
			}
		} else {
			m.msg = m.spinner.View()
			m.activeModel = nil
			m.state = stateSpotifydLoggingIn
			return m, tea.Batch(m.waitForSpotifydPlayerDevice, m.spinner.Tick)
		}

	case libspotify.PlayerDevice:
		m.state = stateSpotifydLogged
		m.spotifydPlayer = &msg
		mainModel := player.NewModel(
			m.WindowSize,
			m.ctx,
			m.spotifyClient,
			m.spotifyUser,
			m.spotifydPlayer,
		)
		m.player = &mainModel
		return m, m.player.Init()

	case filedownloader.DownloadFinished:
		if !spotifyd.IsBinaryValid() {
			fmt.Println("spotifyd checksum is invalid")
			os.Exit(1)
		}
		m.state = stateSpotifydConfig
		m.activeModel = choice.NewModel(
			m.WindowSize,
			spotifydDownloadMsg,
			spotifydConfigChoices,
		)
		return m, nil

	case choice.Selected:
		m.state = spotifydConfigChoicesStates[string(msg)]
		switch m.state {
		case stateSpotifydUserPasswordSelected:
			m.activeModel = spotifydview.NewLoginModel(
				m.spotifyUser.Email,
				m.WindowSize,
			)
			return m, nil
		default:
			m.activeModel = nil
			m.msg = fmt.Sprintf(spotifydUseApp, spotifyd.DeviceName())
			return m, m.spotifydLoginDiscovery
		}

	default:
		var cmd tea.Cmd
		switch m.state {

		case stateSpotifydDownload:
			m.filedownloader, cmd = m.filedownloader.Update(msg)
			m.msg = m.filedownloader.View()
			return m, cmd

		case stateInit, stateSpotifydLoggingIn:
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

func (m Model) webAPILogin() tea.Msg {
	return spotify.Login(m.ctx)
}

func (m Model) webAPIAuth() tea.Msg {
	return spotify.Authorize()
}

func (m Model) getCurrentUser() tea.Msg {
	u, err := m.spotifyClient.CurrentUser(m.ctx)
	if err != nil {
		panic(err)
	}

	return u
}

func (m Model) spotifydLogin() tea.Msg {
	return spotifyd.LoginWithCredentials()
}

func (m Model) spotifydLoginDiscovery() tea.Msg {
	return spotifyd.LoginDiscovery()
}

func (m Model) waitForSpotifydPlayerDevice() tea.Msg {
	for attempt := 0; attempt < 10; attempt++ {
		devices, err := m.spotifyClient.PlayerDevices(m.ctx)
		if err != nil {
			panic(err)
		}
		idx := slices.IndexFunc(devices, func(e libspotify.PlayerDevice) bool {
			return e.Name == spotifyd.DeviceName()
		})
		if idx > -1 {
			return devices[idx]
		}
		time.Sleep(1 * time.Second)
	}
	panic("Wating for spotifyd connection has timeout")
}
