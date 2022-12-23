package ui

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/spotifyd"
	"github.com/spot-go/spot-go/internal/ui/components/choice"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"
	spotifydview "github.com/spot-go/spot-go/internal/ui/views/spotifyd"
	libspotify "github.com/zmb3/spotify/v2"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"golang.design/x/clipboard"
)

type (
	Model struct {
		ctx            context.Context
		spotifyClient  *spotify.Client
		spotifyUser    *libspotify.PrivateUser
		screenWidth    int
		screenHeigth   int
		msg            string
		state          state
		spinner        spinner.Model
		activeModel    tea.Model
		filedownloader filedownloader.Model
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
)

var (
	spotifydConfigChoicesStates = map[string]state{
		"Type username and password": stateSpotifydUserPasswordSelected,

		"Use an app (desktop or mobile) connected on your local network": stateSpotifydAppOnLocalNetworkSelected,
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
		ctx:          context.Background(),
		screenWidth:  0,
		screenHeigth: 0,
		msg:          s.View(),
		spinner:      s,
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
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
		m.screenHeigth = msg.Height
		m.screenWidth = msg.Width
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
			m.state = stateWebAPILogged
			m.msg = fmt.Sprintf(webAPIWelcomeMsg, m.spotifyUser.DisplayName)
		} else {
			m.state = stateSpotifydLogged
			m.msg = "spotifyd logged"
		}
		return m, nil

	case filedownloader.DownloadFinished:
		if !spotifyd.IsBinaryValid() {
			fmt.Println("spotifyd checksum is invalid")
			os.Exit(1)
		}
		m.state = stateSpotifydConfig
		m.activeModel = choice.NewModel(
			m.screenWidth,
			m.screenHeigth,
			spotifydDownloadMsg,
			spotifydConfigChoices,
		)
		return m, nil

	case choice.Selected:
		m.state = spotifydConfigChoicesStates[string(msg)]
		switch m.state {
		case stateSpotifydUserPasswordSelected:
			m.activeModel = spotifydview.NewLoginModel(m.screenWidth, m.screenHeigth)
		default:
			m.activeModel = nil
			m.msg = fmt.Sprintf("Selected: %s", msg)
		}
		return m, nil

	default:
		var cmd tea.Cmd
		switch m.state {

		case stateSpotifydDownload:
			m.filedownloader, cmd = m.filedownloader.Update(msg)
			m.msg = m.filedownloader.View()
			return m, cmd

		case stateInit:
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
	if m.activeModel != nil {
		return m.activeModel.View()
	}

	return lipgloss.Place(
		m.screenWidth,
		m.screenHeigth,
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
