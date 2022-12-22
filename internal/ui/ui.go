package ui

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/spotify"
	"github.com/spot-go/spot-go/internal/ui/components/filedownloader"

	"golang.design/x/clipboard"
)

type (
	Model struct {
		ctx            context.Context
		width          int
		height         int
		msg            string
		state          state
		spinner        spinner.Model
		filedownloader filedownloader.Model
	}

	state int
)

const (
	stateInit state = iota
	stateNeedsAuth
	stateCopyAuthURL
	stateLoggingIn
	stateLogged
	stateSpotifydDownload
	stateSpotifydDownloaded

	webAPILoginMsg = `Let's login to spotify. First we need a token to use the Web API.
Press enter to open a browser and login.`
	webAPIAuthMsg = `A browser should be opened. Continue the login to spotify there.

If the browser did not open, press "c" to copy the URL to your clipboard
and past it on your browser manually.`
	webAPIURLCopiedToClipboardMsg = "URL copied to clipboard. Paste it on your browser and continue the login."
	webAPIWelcomeMsg              = `Welcome %s.

Now we are going to configure the spotify player.
Press enter to continue`
)

func NewModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return Model{
		ctx:     context.Background(),
		width:   0,
		height:  0,
		msg:     s.View(),
		spinner: s,
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
		case "enter":
			switch m.state {
			case stateLogged:
				m.filedownloader = filedownloader.NewModel("https://github.com/Spotifyd/spotifyd/releases/download/v0.3.4/spotifyd-linux-full.tar.gz")
				m.msg = m.filedownloader.View()
				m.state = stateSpotifydDownload
				return m, m.filedownloader.Init()
			case stateNeedsAuth:
				m.state = stateLoggingIn
				m.msg = webAPIAuthMsg
				return m, m.webAPIAuth
			default:
				return m, nil
			}
		case "c":
			switch m.state {
			case stateLoggingIn:
				clipboard.Write(clipboard.FmtText, []byte(spotify.AuthURL))
				m.state = stateCopyAuthURL
				m.msg = webAPIURLCopiedToClipboardMsg
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		if m.state == stateInit {
			return m, m.webAPILogin
		}
		return m, nil

	case spotify.ClientResponse:
		if errors.Is(msg.Err, spotify.ErrNeedsAuth) {
			m.msg = webAPILoginMsg
			m.state = stateNeedsAuth
			return m, nil
		}

		if msg.Err != nil {
			fmt.Println(msg.Err)
			os.Exit(1)
		}
		u, err := msg.Client.CurrentUser(m.ctx)
		if err != nil {
			panic(err)
		}
		m.state = stateLogged
		m.msg = fmt.Sprintf(webAPIWelcomeMsg, u.DisplayName)
		return m, nil

	case filedownloader.DownloadFinished:
		if !config.IsSpotifydFileValid() {
			fmt.Println("spotifyd checksum is invalid")
			os.Exit(1)
		}
		m.state = stateSpotifydDownloaded
		m.msg = "Spotifyd downloaded"
		return m, nil

	default:
		var cmd tea.Cmd
		if m.state == stateSpotifydDownload {
			m.filedownloader, cmd = m.filedownloader.Update(msg)
			m.msg = m.filedownloader.View()
			return m, cmd
		}

		if m.state != stateInit {
			return m, nil
		}
		m.spinner, cmd = m.spinner.Update(msg)
		m.msg = m.spinner.View()
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	return lipgloss.Place(
		m.width,
		m.height,
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
