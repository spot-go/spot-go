package background

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spot-go/spot-go/internal/command"
)

func useTempFolder(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	oldPid, oldLog, oldTimeout := pidFile, logFile, stopTimeout
	pidFile = filepath.Join(dir, "background.pid")
	logFile = filepath.Join(dir, "background.log")
	t.Cleanup(func() { pidFile, logFile, stopTimeout = oldPid, oldLog, oldTimeout })
	return dir
}

// startFake starts a shell that runs script with the command line of spot-go
// in the background, as Start would, and returns its process ID once script
// started. script writes "started" as it starts.
func startFake(t *testing.T, script string) int {
	t.Helper()

	p, err := start("sh", append([]string{"-c", script, "spot-go"}, Args(4242, "soloist-device")...))
	if err != nil {
		t.Fatal(err)
	}
	// As the parent, the test reaps it once it ends.
	exited := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = p.Kill()
		<-exited
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, _ := os.ReadFile(logFile); strings.Contains(string(b), "started") {
			return p.Pid()
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake didn't start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitStopped(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for command.Running(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%d still runs", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestArgs(t *testing.T) {
	got := Args(4242, "soloist-device")
	want := []string{"background", "--soloist", "4242", "--device", "soloist-device"}
	if !slices.Equal(got, want) {
		t.Fatalf("Args() = %q, want %q", got, want)
	}
}

func TestStartSavesThePidAndStopStopsIt(t *testing.T) {
	useTempFolder(t)
	pid := startFake(t, `echo started; while :; do sleep 0.05; done`)

	if got, ok := pidInFile(); !ok || got != pid {
		t.Fatalf("pid file = %d, %t, want %d", got, ok, pid)
	}
	if got, ok := followed(pid); !ok || got != 4242 {
		line, _ := command.Line(pid)
		t.Fatalf("followed(%d) = %d, %t for %q, want the Soloist 4242", pid, got, ok, line)
	}

	if got := Stop(); got != 4242 {
		t.Fatalf("Stop() = %d, want the Soloist it followed, 4242", got)
	}
	waitStopped(t, pid)
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the pid file is still there after Stop(): %v", err)
	}
}

func TestStopKillsWhatDoesntEnd(t *testing.T) {
	useTempFolder(t)
	stopTimeout = 100 * time.Millisecond
	pid := startFake(t, `trap '' TERM; echo started; while :; do sleep 0.05; done`)

	start := time.Now()
	Stop()
	waitStopped(t, pid)
	if took := time.Since(start); took < stopTimeout {
		t.Fatalf("Stop() killed it after %v, want it to wait %v", took, stopTimeout)
	}
}

func TestStopLeavesOtherProgramsAlone(t *testing.T) {
	useTempFolder(t)
	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = other.Process.Kill()
		_ = other.Wait()
	})
	// The process ID of spot-go in the background went to another program.
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := Stop(); got != 0 {
		t.Fatalf("Stop() = %d for another program, want 0", got)
	}
	if !command.Running(other.Process.Pid) {
		t.Fatal("Stop() stopped another program")
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the pid file is still there after Stop(): %v", err)
	}
}

func TestStopWithNothingInTheBackground(t *testing.T) {
	useTempFolder(t)

	if got := Stop(); got != 0 {
		t.Fatalf("Stop() = %d with no pid file, want 0", got)
	}
	if err := os.WriteFile(pidFile, []byte("not a pid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Stop(); got != 0 {
		t.Fatalf("Stop() = %d with a broken pid file, want 0", got)
	}
}

func TestStartKillsWhatItCouldNotSave(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep is not installed")
	}
	dir := useTempFolder(t)
	pidFile = filepath.Join(dir, "missing", "background.pid")
	marker := fmt.Sprintf("spot-go-unsaved-%d", os.Getpid())

	if _, err := start("sh", []string{"-c", "while :; do sleep 0.05; done", marker}); err == nil {
		t.Fatal("start() = nil without a pid file the next run can stop it with")
	}
	deadline := time.Now().Add(5 * time.Second)
	for exec.Command("pgrep", "-f", marker).Run() == nil {
		if time.Now().After(deadline) {
			t.Fatal("start() left running what no run of spot-go could stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartStopsTheOneBefore(t *testing.T) {
	useTempFolder(t)
	script := `echo started; while :; do sleep 0.05; done`
	first := startFake(t, script)

	// As when two runs of spot-go quit, one after the other.
	second := startFake(t, script)
	waitStopped(t, first)
	if got, ok := pidInFile(); !ok || got != second {
		t.Fatalf("pid file = %d, %t, want %d", got, ok, second)
	}
}

func TestRunEndsWithoutTheSoloistLeftPlaying(t *testing.T) {
	useTempFolder(t)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// No Soloist runs with process ID -1, so Run ends before it logs in.
	if err := Run(context.Background(), -1, ""); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Run() left its pid file: %v", err)
	}
}

func TestForgetRemovesOnlyItsOwnPid(t *testing.T) {
	useTempFolder(t)

	if err := os.WriteFile(pidFile, []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	forget()
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("forget() removed the pid file of another process: %v", err)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	forget()
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forget() left its own pid file: %v", err)
	}
}
