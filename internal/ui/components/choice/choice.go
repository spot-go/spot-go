package choice

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type (
	Model struct {
		title        string
		choices      []string
		screenWidth  int
		screenHeigth int
		cursor       int
	}
	Selected string
)

func NewModel(screenWidth, screenHeigth int, title string, choices []string) Model {
	return Model{
		screenWidth:  screenWidth,
		screenHeigth: screenHeigth,
		title:        title,
		choices:      choices,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.screenHeigth = msg.Height
		m.screenWidth = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m, func() tea.Msg {
				return Selected(m.choices[m.cursor])
			}

		case "down":
			m.cursor++
			if m.cursor >= len(m.choices) {
				m.cursor = 0
			}

		case "up":
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(m.choices) - 1
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	s := strings.Builder{}

	for i := 0; i < len(m.choices); i++ {
		if m.cursor == i {
			s.WriteString("(•) ")
		} else {
			s.WriteString("( ) ")
		}
		s.WriteString(m.choices[i])
		s.WriteString("\n")
	}
	body := lipgloss.NewStyle().
		Align(lipgloss.Left, lipgloss.Center).
		Render(s.String())

	return lipgloss.Place(
		m.screenWidth,
		m.screenHeigth,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s\n%s", m.title, body),
	)
}
