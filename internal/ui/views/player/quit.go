package player

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spot-go/spot-go/internal/ui/styles"
)

type (
	// quitPrompt asks, when quitting while Spotify plays on spot-go, whether
	// the music keeps playing after spot-go quits.
	quitPrompt struct {
		open   bool
		choice quitChoice
	}

	quitChoice int
)

const (
	keepPlayingChoice quitChoice = iota
	stopChoice
	cancelChoice
	quitChoiceCount

	quitQuestion = "Keep the music playing after you quit?"
	quitDetail   = "Control it from the Spotify app, or open spot-go again."
)

var (
	quitChoiceNames = [quitChoiceCount]string{"Keep playing", "Stop", "Cancel"}

	quitHints = []playerHint{
		{"k", "keep playing"},
		{"s", "stop"},
		{"esc", "cancel"},
	}

	quitBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.LipglossColorForegroundGreenSpotify).
			Padding(1, 2)
	quitQuestionStyle = playingStyle.Bold(true)
	quitChoiceStyle   = playingStyle.Padding(0, 1)
	quitChosenStyle   = lipgloss.NewStyle().
				Background(styles.LipglossColorBackgroundGreenSpotify).
				Foreground(styles.LipglossColorBlack).
				Padding(0, 1)
)

// KeepPlaying reports whether the user chose to keep the music playing after
// spot-go quits.
func (m Model) KeepPlaying() bool {
	return m.keepPlaying
}

// quit quits spot-go. While Spotify plays on spot-go, it first asks whether
// the music keeps playing.
func (m Model) quit() (Model, tea.Cmd) {
	if !m.ownPlaying() {
		return m, tea.Quit
	}
	m.quitPrompt = quitPrompt{open: true}
	return m, nil
}

// quitInBackground quits spot-go right away, with no prompt. While Spotify
// plays on spot-go, the music keeps playing, as with Keep playing on the
// quit prompt. Otherwise spot-go quits as when the prompt doesn't show.
func (m Model) quitInBackground() (Model, tea.Cmd) {
	m.keepPlaying = m.ownPlaying()
	return m, tea.Quit
}

// ownPlaying reports whether Spotify plays on spot-go's own device, as far as
// spot-go knows: from the last poll, or the last change made in spot-go
// since. When spot-go knows it plays but not where, as right after it
// resumed with no active device, it plays on spot-go. Before spot-go knows
// what plays, spot-go's device plays when Spotify said it was the active one
// as spot-go started, as when an earlier run of spot-go left it playing.
func (m Model) ownPlaying() bool {
	own := m.ownDevice()
	if own == "" {
		return false
	}
	if !m.nowPlaying.info.Playing {
		return !m.knowsPlayback && m.playerDevice.Active
	}
	return m.playerBar.device == "" || m.playerBar.device == own
}

// quitKey handles a key pressed while the quit prompt shows. It takes every
// key, so none reaches the tabs.
func (m Model) quitKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "left":
		m.quitPrompt.choice = max(0, m.quitPrompt.choice-1)
	case "right":
		m.quitPrompt.choice = min(quitChoiceCount-1, m.quitPrompt.choice+1)
	case "tab":
		m.quitPrompt.choice = (m.quitPrompt.choice + 1) % quitChoiceCount
	case "shift+tab":
		m.quitPrompt.choice = (m.quitPrompt.choice + quitChoiceCount - 1) % quitChoiceCount
	case "enter":
		return m.chooseQuit(m.quitPrompt.choice)
	case "k", "ctrl+k":
		return m.chooseQuit(keepPlayingChoice)
	case "s", "ctrl+c", "ctrl+q":
		// Quitting again quits as spot-go always did: the music stops.
		return m.chooseQuit(stopChoice)
	case "esc":
		return m.chooseQuit(cancelChoice)
	}
	return m, nil
}

func (m Model) chooseQuit(choice quitChoice) (Model, tea.Cmd) {
	switch choice {
	case keepPlayingChoice:
		m.keepPlaying = true
		return m, tea.Quit
	case stopChoice:
		return m, tea.Quit
	}
	m.quitPrompt = quitPrompt{}
	return m, nil
}

// view is the quit prompt in a box at the center of an area width columns
// wide and height lines high. When the box doesn't fit, the prompt leaves out
// the detail, and then the box. The detail shows in the gray of what the
// keys under it do, so it reads as well as they do.
func (q quitPrompt) view(width, height int) string {
	h := styles.Help()
	question, detail, choices, hints := quitQuestionStyle.Render(quitQuestion), h.ShortDesc.Render(quitDetail), q.choices(), hintText(h, quitHints)
	boxes := []string{
		quitBoxStyle.Render(strings.Join([]string{question, "", detail, "", choices, "", hints}, "\n")),
		quitBoxStyle.Render(strings.Join([]string{question, "", choices, "", hints}, "\n")),
	}
	for _, box := range boxes {
		if lipgloss.Width(box) <= width && lipgloss.Height(box) <= height {
			return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
		}
	}

	lines := []string{question, choices, hints}
	for i, line := range lines {
		lines[i] = fit(line, width)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

// choices are the buttons of the prompt, with the chosen one lit.
func (q quitPrompt) choices() string {
	buttons := make([]string, quitChoiceCount)
	for i, name := range quitChoiceNames {
		style := quitChoiceStyle
		if quitChoice(i) == q.choice {
			style = quitChosenStyle
		}
		buttons[i] = style.Render(name)
	}
	return strings.Join(buttons, "  ")
}
