package filedownloader

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spot-go/spot-go/internal/config"
)

const (
	padding  = 2
	maxWidth = 80
)

type Model struct {
	progress progress.Model
	url      string
	dstFile  string
	err      error
}

func NewModel(url string) Model {
	dst := path.Join(config.Path, filepath.Base(url))

	return Model{
		url:      url,
		dstFile:  dst,
		progress: progress.New(progress.WithDefaultGradient()),
	}
}

func (m Model) Init() tea.Cmd {
	return startDownload(m.url, m.dstFile)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - padding*2 - 4
		if m.progress.Width > maxWidth {
			m.progress.Width = maxWidth
		}
		return m, nil

	case progressMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if msg.value >= 1.0 {
			return m, nil
		}

		return m, m.progress.SetPercent(float64(msg.value))

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
		fmt.Printf("Fail to download file from url %s: %s\n", m.url, m.err.Error())
		os.Exit(1)
	}

	return fmt.Sprintf("Downloading file...\n%s", m.progress.View())
}
