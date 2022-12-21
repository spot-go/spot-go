package filedownloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	padding  = 2
	maxWidth = 80
)

type progressMsg float64

type progressErrMsg struct{ err error }

func finalPause() tea.Cmd {
	return tea.Tick(time.Millisecond*750, func(_ time.Time) tea.Msg {
		return nil
	})
}

type Model struct {
	pw       *progressWriter
	progress progress.Model
	err      error
}

func NewModel(url string) Model {
	resp, err := getResponse(url)
	if err != nil {
		fmt.Println("could not get response", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.ContentLength <= 0 {
		fmt.Println("can't parse content length, aborting download")
		os.Exit(1)
	}

	filename := filepath.Base(url)
	file, err := os.Create(filename)
	if err != nil {
		fmt.Println("could not create file:", err)
		os.Exit(1)
	}
	defer file.Close()

	pw := &progressWriter{
		total:  int(resp.ContentLength),
		file:   file,
		reader: resp.Body,
	}
	m := Model{
		pw:       pw,
		progress: progress.New(progress.WithDefaultGradient()),
	}
	pw.onProgress = func(ratio float64) {
		m.Update(progressMsg(ratio))
	}

	go pw.start(m)

	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - padding*2 - 4
		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}
		return m, nil

	case progressErrMsg:
		m.err = msg.err
		return m, nil

	case progressMsg:
		var cmds []tea.Cmd

		if msg >= 1.0 {
			cmds = append(cmds, tea.Sequence(finalPause(), tea.Quit))
		}

		cmds = append(cmds, m.progress.SetPercent(float64(msg)))
		return m, tea.Batch(cmds...)

	// FrameMsg is sent when the progress bar wants to animate itself
	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	default:
		return m, nil
	}
}

func (m Model) View() string {
	if m.err != nil {
		return "Error downloading: " + m.err.Error() + "\n"
	}

	pad := strings.Repeat("", padding)
	return pad + m.progress.View()
}
