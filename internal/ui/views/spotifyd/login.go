package spotifyd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotifyd"
)

const (
	title = "Fill your password below (your credentials are not saved in the local disk)"
)

var (
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#1DB954"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	cursorStyle  = focusedStyle.Copy()
	noStyle      = lipgloss.NewStyle()

	focusedButton = focusedStyle.Copy().Render("[ Submit ]")
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	blurredButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Submit"))
)

type (
	LoginModel struct {
		focusIndex   int
		password     textinput.Model
		email        string
		screenWidth  int
		screenHeigth int
		errorMsg     string
	}
)

func NewLoginModel(email string, screenWidth, screenHeigth int) LoginModel {
	m := LoginModel{
		screenWidth:  screenWidth,
		screenHeigth: screenHeigth,
		email:        email,
	}

	t := textinput.New()
	t.CursorStyle = cursorStyle
	t.CharLimit = 128
	t.Placeholder = "Password"
	t.EchoMode = textinput.EchoPassword
	t.EchoCharacter = '•'
	t.Focus()

	m.password = t
	return m
}

func (m LoginModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m LoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.screenHeigth = msg.Height
		m.screenWidth = msg.Width
		return m, nil

	case spotifyd.LoginResponse:
		if msg.Err != nil {
			m.errorMsg = msg.Err.Error()
			return m, nil
		}
		m.errorMsg = "Ok"
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			if s == "enter" && m.focusIndex == 1 {
				pass := m.password.Value()
				if strings.Trim(pass, " ") == "" {
					m.errorMsg = "Password cannot be empty"
					return m, nil
				}

				m.errorMsg = ""
				return m, m.Login(m.email, pass)
			}

			// Cycle indexes
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > 1 {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = 1
			}

			if m.focusIndex == 0 {
				// Set focused state
				cmd := m.password.Focus()
				m.password.PromptStyle = focusedStyle
				m.password.TextStyle = focusedStyle
				return m, cmd
			} else {
				// Remove focused state
				m.password.Blur()
				m.password.PromptStyle = noStyle
				m.password.TextStyle = noStyle
			}

			return m, nil
		}
	}

	// Handle character input and blinking
	var cmd tea.Cmd
	m.password, cmd = m.password.Update(msg)

	return m, cmd
}

func (m LoginModel) View() string {
	button := &blurredButton
	if m.focusIndex == 1 {
		button = &focusedButton
	}
	form := fmt.Sprintf(
		"> %s\n%s\n%s\n%s\n\n",
		m.email,
		m.password.View(),
		errorStyle.Render(m.errorMsg),
		*button,
	)

	content := lipgloss.NewStyle().
		Align(lipgloss.Left, lipgloss.Center).
		Render(fmt.Sprintf("%s\n\n%s", title, form))

	return lipgloss.Place(
		m.screenWidth,
		m.screenHeigth,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}

func (m LoginModel) Login(email, pass string) tea.Cmd {
	return func() tea.Msg {
		return spotifyd.LoginWithUserAndPassword(email, pass)
	}
}
