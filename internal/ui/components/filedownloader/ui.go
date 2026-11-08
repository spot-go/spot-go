package filedownloader

import (
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	padding  = 2
	maxWidth = 80
)

type (
	Model struct {
		progress progress.Model
		url      string
		dstFile  string
		name     string
		install  func(file string) error
		err      error
	}
	DownloadFinished bool
	DownloadFailed   struct {
		Err error
	}
)

func finalPause() tea.Cmd {
	return tea.Tick(1*time.Second, func(_ time.Time) tea.Msg {
		return nil
	})
}

func NewModel(url, dst, name string, install func(file string) error) Model {
	return Model{
		url:      url,
		dstFile:  path.Join(dst, filepath.Base(url)),
		name:     name,
		install:  install,
		progress: progress.New(progress.WithDefaultGradient()),
	}
}

func (m Model) Init() tea.Cmd {
	return startDownload(m.url, m.dstFile, m.name, m.install)
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
			return m, func() tea.Msg {
				return DownloadFailed{msg.err}
			}
		}

		var cmds []tea.Cmd
		if msg.done {
			cmds = append(cmds, tea.Sequence(finalPause(), func() tea.Msg {
				return DownloadFinished(true)
			}))
		}

		cmds = append(cmds, m.progress.SetPercent(float64(msg.value)))
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
		return fmt.Sprintf("Fail to download file from url %s: %s", m.url, m.err.Error())
	}

	return fmt.Sprintf("Downloading %s...\n%s", m.name, m.progress.View())
}
