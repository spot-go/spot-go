package media

import (
	"fmt"
	"os"
	"testing"
	"time"
)

type testRun struct {
	code     int
	finished time.Time
}

func TestMain(m *testing.M) {
	done := make(chan testRun, 1)
	Main(func() {
		code := m.Run()
		done <- testRun{code: code, finished: time.Now()}
	})

	select {
	case run := <-done:
		if wait := time.Since(run.finished); wait > 500*time.Millisecond {
			fmt.Fprintf(os.Stderr, "Main returned %v after its function finished\n", wait)
			run.code = 1
		}
		os.Exit(run.code)
	default:
		fmt.Fprintln(os.Stderr, "Main returned before its function finished")
		os.Exit(1)
	}
}
