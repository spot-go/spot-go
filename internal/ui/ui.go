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
)

type (
	Model struct {
		ctx     context.Context
		width   int
		height  int
		msg     string
		state   state
		spinner spinner.Model
	}

	state int
)

const (
	stateInit state = iota
	stateNeedsAuth
	stateLoggingIn
	stateLogged
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
			if m.state == stateInit || m.state == stateLoggingIn {
				return m, nil
			}
			if m.state == stateLogged {
				return m, tea.Quit
			}
			m.state = stateLoggingIn
			m.msg = "A browser should be opened. Continue the login to spotify there."
			return m, m.auth
		}

	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		return m, m.login

	case spotify.ClientResponse:
		if errors.Is(msg.Err, spotify.ErrNeedsAuth) {
			m.msg = "Let's login to spotify. First we need a token to use the Web API. \nPress enter to open a browser and login."
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
		m.msg = fmt.Sprintf("User name: %+v. User E-mail: %+v.", u.DisplayName, u.Email)
		return m, nil
	default:
		if m.state != stateInit {
			return m, nil
		}
		var cmd tea.Cmd
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

func (m Model) login() tea.Msg {
	return spotify.Login(m.ctx)
}

func (m Model) auth() tea.Msg {
	return spotify.Authorize()
}
