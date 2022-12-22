package choice

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	title   string
	choices []string
	cursor  int
	choice  string
}

func NewModel(title string, choices []string) Model {
	return Model{
		title:   title,
		choices: choices,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			// Send the choice on the channel and exit.
			m.choice = m.choices[m.cursor]
			return m, tea.Quit

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
	title := fmt.Sprintf("%s\n\n", m.title)
	s.WriteString(title)

	for i := 0; i < len(m.choices); i++ {
		if m.cursor == i {
			s.WriteString("(•) ")
		} else {
			s.WriteString("( ) ")
		}
		s.WriteString(m.choices[i])
		s.WriteString("\n")
	}

	return s.String()
}
