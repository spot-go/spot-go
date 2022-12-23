package spotifyd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/spotifyd"
)

const (
	title = "Fill the form below (your username and password are not saved in the local disk)"
)

var (
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#1DB954"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	cursorStyle  = focusedStyle.Copy()
	noStyle      = lipgloss.NewStyle()

	focusedButton = focusedStyle.Copy().Render("[ Submit ]")
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	blurredButton = fmt.Sprintf("[ %s ]", blurredStyle.Render("Submit"))

	emailRegex = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
)

type (
	LoginModel struct {
		focusIndex   int
		inputs       []textinput.Model
		screenWidth  int
		screenHeigth int
		errorMsg     string
	}

	loginResult struct {
		err error
	}
)

func NewLoginModel(screenWidth, screenHeigth int) LoginModel {
	m := LoginModel{
		screenWidth:  screenWidth,
		screenHeigth: screenHeigth,
		inputs:       make([]textinput.Model, 2),
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.CursorStyle = cursorStyle
		t.CharLimit = 128

		switch i {
		case 0:
			t.Placeholder = "E-mail"
			t.Focus()
			t.PromptStyle = focusedStyle
		case 1:
			t.Placeholder = "Password"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
		}

		m.inputs[i] = t
	}

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

	case loginResult:
		if msg.err != nil {
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		m.errorMsg = "Ok"
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			// Did the user press enter while the submit button was focused?
			// If so, exit.
			if s == "enter" && m.focusIndex == len(m.inputs) {
				email := m.inputs[0].Value()
				if strings.Trim(email, " ") == "" {
					m.errorMsg = "E-mail cannot be empty"
					return m, nil
				}
				if !emailRegex.MatchString(email) {
					m.errorMsg = "Invalid e-mail"
					return m, nil
				}

				pass := m.inputs[1].Value()
				if strings.Trim(pass, " ") == "" {
					m.errorMsg = "Password cannot be empty"
					return m, nil
				}

				m.errorMsg = ""
				return m, m.Login(email, pass)
			}

			// Cycle indexes
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > len(m.inputs) {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = len(m.inputs)
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i <= len(m.inputs)-1; i++ {
				if i == m.focusIndex {
					// Set focused state
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = focusedStyle
					m.inputs[i].TextStyle = focusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].Blur()
				m.inputs[i].PromptStyle = noStyle
				m.inputs[i].TextStyle = noStyle
			}

			return m, tea.Batch(cmds...)
		}
	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)

	return m, cmd
}

func (m *LoginModel) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	// Only text inputs with Focus() set will respond, so it's safe to simply
	// update all of them here without any further logic.
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m LoginModel) View() string {
	var b strings.Builder

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		if i < len(m.inputs)-1 {
			b.WriteRune('\n')
		}
	}

	button := &blurredButton
	if m.focusIndex == len(m.inputs) {
		button = &focusedButton
	}
	fmt.Fprintf(&b, "\n%s\n%s\n\n", errorStyle.Render(m.errorMsg), *button)

	content := lipgloss.NewStyle().
		Align(lipgloss.Left, lipgloss.Center).
		Render(fmt.Sprintf("%s\n%s", title, b.String()))

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
		err := spotifyd.LoginWithUserAndPassword(email, pass)
		return loginResult{err}
	}
}
