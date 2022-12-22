package filedownloader

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/program"
	"github.com/spot-go/spot-go/internal/tar"
)

type (
	progressWriter struct {
		total      int
		downloaded int
		onProgress func(f float64)
	}
	progressMsg struct {
		value float64
		err   error
	}
)

func startDownload(url, dst string) tea.Cmd {
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

		err = tar.Extract(dst, config.Path)
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to extract tar file: %+v", err)}
		}

		err = os.Remove(dst)
		if err != nil {
			return progressMsg{err: fmt.Errorf("fail to remove tar file: %+v", err)}
		}

		// program.Instance.Send(Fi)

		return progressMsg{value: 100}
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
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("receiving status of %d for url: %s", resp.StatusCode, url)
	}
	return resp, nil
}
