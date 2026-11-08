package filedownloader

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spot-go/spot-go/internal/program"
)

type (
	progressWriter struct {
		total      int
		downloaded int
		onProgress func(f float64)
	}
	progressMsg struct {
		value float64
		done  bool
		err   error
	}
)

func startDownload(url, dst, name string, install func(file string) error) tea.Cmd {
	return func() tea.Msg {
		resp, err := getResponse(url)
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to get response: %+v", err)}
		}
		defer resp.Body.Close()

		if resp.ContentLength <= 0 {
			return progressMsg{
				err: errors.New("cannot parse content length, aborting download"),
			}
		}

		file, err := os.Create(dst)
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to create file: %+v", err)}
		}
		defer os.Remove(dst)
		defer file.Close()

		pw := &progressWriter{
			total: int(resp.ContentLength),
			onProgress: func(f float64) {
				program.Instance.Send(progressMsg{value: f})
			},
		}
		_, err = io.Copy(file, io.TeeReader(resp.Body, pw))
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to copy file: %+v", err)}
		}
		if err := file.Close(); err != nil {
			return progressMsg{err: fmt.Errorf("fail to close file: %+v", err)}
		}

		err = install(dst)
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to install %s: %+v", name, err)}
		}

		return progressMsg{value: 1, done: true}
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	pw.downloaded += len(p)
	if pw.total > 0 && pw.onProgress != nil {
		progress := float64(pw.downloaded) / float64(pw.total)
		pw.onProgress(progress)
	}
	return len(p), nil
}

func getResponse(url string) (*http.Response, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("receiving status of %d for url: %s", resp.StatusCode, url)
	}
	return resp, nil
}
