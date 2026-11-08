package secretinput

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/ui/styles"
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
	input.SetWidth(inputWidth)
	inputStyles := textinput.DefaultStyles(styles.HasDarkBackground)
	inputStyles.Focused.Prompt = lipgloss.NewStyle()
	inputStyles.Blurred.Prompt = lipgloss.NewStyle()
	inputStyles.Cursor.Color = nil
	inputStyles.Cursor.Blink = false
	input.SetStyles(inputStyles)
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

	case tea.KeyPressMsg:
		if msg.String() == "enter" {
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

func (m Model) View() tea.View {
	return tea.NewView(lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s\n\n%s", m.title, m.input.View()),
	))
}
