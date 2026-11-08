package player

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type placeholderModel struct {
	name   string
	width  int
	height int
}

func placeholderTab(name string) tab {
	return tab{name: name, content: placeholderModel{name: name}}
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

func (p placeholderModel) View() tea.View {
	return tea.NewView(lipgloss.Place(
		p.width,
		p.height,
		lipgloss.Center,
		lipgloss.Center,
		fmt.Sprintf("%s is coming soon", p.name),
	))
}
