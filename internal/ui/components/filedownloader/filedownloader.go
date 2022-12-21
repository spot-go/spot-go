package filedownloader

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

type (
	progressWriter struct {
		total      int
		downloaded int
		file       *os.File
		reader     io.Reader
		onProgress func(float64)
	}
)

func (pw *progressWriter) start(m Model) {
	_, err := io.Copy(pw.file, io.TeeReader(pw.reader, pw))
	if err != nil {
		m.Update(progressErrMsg{err})
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	pw.downloaded += len(p)
	if pw.total > 0 && pw.onProgress != nil {
		pw.onProgress(float64(pw.downloaded) / float64(pw.total))
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
