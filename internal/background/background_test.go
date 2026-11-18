package background

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	oldPid, oldLog, oldProgram, oldTimeout := pidFile, logFile, programFolder, stopTimeout
	pidFile = filepath.Join(dir, "background.pid")
	logFile = filepath.Join(dir, "background.log")
	programFolder = filepath.Join(dir, Command)
	t.Cleanup(func() { pidFile, logFile, programFolder, stopTimeout = oldPid, oldLog, oldProgram, oldTimeout })
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

// writeProgram writes a program, which is only text for the tests, at path.
func writeProgram(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
}

func wantFile(t *testing.T, path, text string) {
	t.Helper()

	if b, err := os.ReadFile(path); err != nil || string(b) != text {
		t.Fatalf("%s = %q, %v, want %q", path, b, err, text)
	}
}

// goRunProgram is where go run builds spot-go, in dir.
func goRunProgram(t *testing.T, dir string) string {
	t.Helper()

	exe := filepath.Join(dir, "go-build2619775426", "b001", "exe", "spot-go")
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestBuiltByGoRun(t *testing.T) {
	for _, tt := range []struct {
		exe  string
		want bool
	}{
		{"/var/folders/yn/_cvr7m5x3pdcq9klvz48dw640000gp/T/go-build2619775426/b001/exe/spot-go", true},
		{"/tmp/go-build1962315458/b001/exe/spot-go", true},
		{"/Users/chico/code/spot-go/spot-go", false},
		{"/opt/homebrew/bin/spot-go", false},
		{"/Users/chico/go/bin/spot-go", false},
		{"spot-go", false},
	} {
		if got := builtByGoRun(tt.exe); got != tt.want {
			t.Errorf("builtByGoRun(%q) = %t, want %t", tt.exe, got, tt.want)
		}
	}
}

func TestKeepCopiesWhatGoRunBuilt(t *testing.T) {
	dir := useTempFolder(t)
	exe := goRunProgram(t, t.TempDir())
	writeProgram(t, exe, "spot-go v1")

	kept := keep(exe)

	if want := filepath.Join(dir, Command, "spot-go"); kept != want {
		t.Fatalf("keep() = %q, want %q", kept, want)
	}
	wantFile(t, kept, "spot-go v1")
	info, err := os.Stat(kept)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("the copy has mode %v, %v, want 0700", info.Mode().Perm(), err)
	}
	// go run removes the program it built once spot-go quits.
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	wantFile(t, kept, "spot-go v1")
}

func TestKeepReplacesTheCopyBefore(t *testing.T) {
	useTempFolder(t)
	exe := goRunProgram(t, t.TempDir())
	writeProgram(t, exe, "spot-go v1")
	kept := keep(exe)
	// spot-go in the background still runs from the copy before.
	running, err := os.Open(kept)
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()

	writeProgram(t, exe, "spot-go v2")
	if again := keep(exe); again != kept {
		t.Fatalf("keep() = %q, want %q again", again, kept)
	}

	wantFile(t, kept, "spot-go v2")
	if b, err := io.ReadAll(running); err != nil || string(b) != "spot-go v1" {
		t.Fatalf("the copy before = %q, %v, want it untouched", b, err)
	}
	entries, err := os.ReadDir(programFolder)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%s has %v, %v, want only spot-go", programFolder, entries, err)
	}
}

func TestKeepRunsWhatGoRunBuiltWhenItCantCopyIt(t *testing.T) {
	dir := useTempFolder(t)
	exe := goRunProgram(t, t.TempDir())
	writeProgram(t, exe, "spot-go v1")
	// A file where the folder of the copy should be.
	programFolder = filepath.Join(dir, "file")
	writeProgram(t, programFolder, "")

	if got := keep(exe); got != exe {
		t.Fatalf("keep() = %q, want %q", got, exe)
	}
	missing := filepath.Join(filepath.Dir(exe), "missing")
	if got := keep(missing); got != missing {
		t.Fatalf("keep() of a missing program = %q, want it", got)
	}
}

func TestKeepRunsAProgramGoRunDidntBuild(t *testing.T) {
	useTempFolder(t)
	// A copy an earlier go run left.
	keep(writtenGoRunProgram(t))
	exe := filepath.Join(t.TempDir(), "spot-go")
	writeProgram(t, exe, "spot-go v2")

	if got := keep(exe); got != exe {
		t.Fatalf("keep() = %q, want %q", got, exe)
	}
	if _, err := os.Stat(programFolder); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s is still there (%v), want the copy gone", programFolder, err)
	}
}

func TestKeepLeavesTheCopyItRunsFrom(t *testing.T) {
	useTempFolder(t)
	kept := keep(writtenGoRunProgram(t))

	if got := keep(kept); got != kept {
		t.Fatalf("keep() = %q, want %q", got, kept)
	}
	wantFile(t, kept, "spot-go v1")
}

func writtenGoRunProgram(t *testing.T) string {
	t.Helper()

	exe := goRunProgram(t, t.TempDir())
	writeProgram(t, exe, "spot-go v1")
	return exe
}
