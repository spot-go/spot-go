package spotifyd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotifyd"
	"github.com/spot-go/spot-go/internal/ui/styles"
	"github.com/spot-go/spot-go/internal/ui/views"
)

const (
	title = "Fill your password below (your credentials are not saved in the local disk)"
)

var (
	focusedStyle = lipgloss.NewStyle().Foreground(styles.LipglossColorForegroundGreenSpotify)
	errorStyle   = lipgloss.NewStyle().Foreground(styles.LipglossColorRed)
	cursorStyle  = focusedStyle.Copy()
	noStyle      = lipgloss.NewStyle()

	focusedButton = focusedStyle.Copy().Render("[ Submit ]")
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#585858"))
	blurredButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Submit"))
)

type (
	LoginModel struct {
		views.WindowSize
		focusIndex    int
		passwordInput textinput.Model
		spinner       spinner.Model
		isLoggingIn   bool
		email         string
		errorMsg      string
	}
)

func NewLoginModel(email string, windowsSize views.WindowSize) LoginModel {
	passwordInput := textinput.New()
	passwordInput.CursorStyle = cursorStyle
	passwordInput.CharLimit = 128
	passwordInput.Placeholder = "Password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '•'
	passwordInput.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot

	return LoginModel{
		WindowSize:    windowsSize,
		email:         email,
		passwordInput: passwordInput,
		spinner:       s,
	}
}

func (m LoginModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m LoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		return m, nil

	case spotifyd.LoginResponse:
		m.isLoggingIn = false
		if msg.Err != nil {
			m.errorMsg = msg.Err.Error()
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			if s == "enter" && m.focusIndex == 1 {
				pass := m.passwordInput.Value()
				if strings.Trim(pass, " ") == "" {
					m.errorMsg = "Password cannot be empty"
					return m, m.spinner.Tick
				}

				m.isLoggingIn = true
				m.errorMsg = ""
				return m, tea.Batch(m.Login(m.email, pass), m.spinner.Tick)
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
				cmd := m.passwordInput.Focus()
				m.passwordInput.PromptStyle = focusedStyle
				m.passwordInput.TextStyle = focusedStyle
				return m, cmd
			} else {
				// Remove focused state
				m.passwordInput.Blur()
				m.passwordInput.PromptStyle = noStyle
				m.passwordInput.TextStyle = noStyle
			}

			return m, m.spinner.Tick
		}
	}

	// Handle character input and blinking
	cmds := make([]tea.Cmd, 0)
	input, cmd := m.passwordInput.Update(msg)
	m.passwordInput = input
	cmds = append(cmds, cmd)

	if m.isLoggingIn {
		input, cmd := m.spinner.Update(msg)
		m.spinner = input
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m LoginModel) View() string {
	button := &blurredButton
	if m.focusIndex == 1 {
		button = &focusedButton
	}

	var status string
	if m.isLoggingIn {
		status = m.spinner.View()
	} else {
		status = errorStyle.Render(m.errorMsg)
	}

	form := fmt.Sprintf(
		"> %s\n%s\n%s\n%s\n\n",
		m.email,
		m.passwordInput.View(),
		status,
		*button,
	)

	content := lipgloss.NewStyle().
		Align(lipgloss.Left, lipgloss.Center).
		Render(fmt.Sprintf("%s\n\n%s", title, form))

	return lipgloss.Place(
		m.Width,
		m.Height,
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
