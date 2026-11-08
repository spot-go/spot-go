package secretinput

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spot-go/spot-go/internal/ui/views"
)

const inputWidth = 48

type (
	Model struct {
		views.WindowSize
		title string
		input textinput.Model
	}
	Submitted string
)

func NewModel(windowSize views.WindowSize, title, placeholder string) Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.Width = inputWidth
	input.SetCursorMode(textinput.CursorStatic)
	input.Focus()

	return Model{
		WindowSize: windowSize,
		title:      title,
		input:      input,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) CapturesInput() bool {
	return true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.WindowSizeMsg = msg
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyEnter {
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return Submitted(value)
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s\n\n%s", m.title, m.input.View()),
	)
}
