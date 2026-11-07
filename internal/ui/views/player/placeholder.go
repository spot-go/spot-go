package player

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type placeholderModel struct {
	name   string
	width  int
	height int
}

func newPlaceholderModel(name string) placeholderModel {
	return placeholderModel{name: name}
}

func (p placeholderModel) Init() tea.Cmd {
	return nil
}

func (p placeholderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cd, ok := msg.(contentDimension); ok {
		p.width = cd.width
		p.height = cd.height
	}
	return p, nil
}

func (p placeholderModel) View() string {
	return lipgloss.Place(
		p.width,
		p.height,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s is coming soon", p.name),
	)
}
