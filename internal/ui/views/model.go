package views

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type WindowSize struct {
	tea.WindowSizeMsg
}

func FullScreen(content string, height int) tea.View {
	v := tea.NewView(lastLines(content, height))
	v.AltScreen = true
	return v
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n <= 0 || len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
