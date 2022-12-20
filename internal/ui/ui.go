package ui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotify"
)

type (
	Model struct {
		ctx    context.Context
		ready  bool
		width  int
		height int
		msg    string
		state  state
	}

	state int
)

const (
	stateInit state = iota
	stateLoggingIn
	stateLogged
)

func NewModel() Model {
	return Model{
		ctx:    context.Background(),
		ready:  false,
		width:  0,
		height: 0,
		msg:    "Let's login to spotify. First we need a token to use the Web API. \nPress enter to open a browser and login."}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.state == stateLoggingIn {
				return m, nil
			}
			if m.state == stateLogged {
				return m, tea.Quit
			}
			m.state = stateLoggingIn
			m.msg = "A browser should be opened. Continue the login to spotify there."
			return m, m.login
		}

	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.ready = true

	case spotify.ClientResponse:
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
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}
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
