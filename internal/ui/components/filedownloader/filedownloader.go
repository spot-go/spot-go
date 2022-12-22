package filedownloader

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spot-go/spot-go/internal/config"
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
		err   error
	}
)

func startDownload(url string) tea.Cmd {
	return func() tea.Msg {
		resp, err := getResponse(url)
		if err != nil {
			fmt.Println("Could not get response", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.ContentLength <= 0 {
			fmt.Println("Cannot parse content length, aborting download")
			os.Exit(1)
		}

		filename := path.Join(config.Path, filepath.Base(url))
		file, err := os.Create(filename)
		if err != nil {
			fmt.Println("Could not create file:", err)
			os.Exit(1)
		}
		defer file.Close()

		pw := &progressWriter{
			total: int(resp.ContentLength),
			onProgress: func(f float64) {
				program.Instance.Send(progressMsg{value: f})
			},
		}
		_, err = io.Copy(file, io.TeeReader(resp.Body, pw))
		return progressMsg{0, err}
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
