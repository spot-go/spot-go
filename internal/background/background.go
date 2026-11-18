// Package background keeps Now Playing and the media keys on the music
// Soloist plays once spot-go quits and leaves it playing. spot-go starts
// itself again for that, in the background, and the next run of spot-go
// stops it.
package background

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spot-go/spot-go/internal/command"
	"github.com/spot-go/spot-go/internal/config"
	"github.com/spot-go/spot-go/internal/media"
	"github.com/spot-go/spot-go/internal/soloist"
	"github.com/spot-go/spot-go/internal/spotify"
	libspotify "github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

const (
	// Command is the hidden spot-go command that runs in the background.
	Command = "background"
	// SoloistFlag and DeviceFlag are its flags: the process ID of the
	// Soloist left playing, and its device ID.
	SoloistFlag = "soloist"
	DeviceFlag  = "device"

	stopEvery = 20 * time.Millisecond
	// refreshTimeout is how long a refresh of the Spotify token can take.
	// It is long, as Spotify may have given a new token by then, and an
	// answer cut short loses it.
	refreshTimeout = 30 * time.Second
)

var (
	// stopTimeout is how long Stop waits for spot-go in the background to
	// end before it kills it. It usually ends at once, but a token refresh
	// it runs ends first, so the new token gets saved.
	stopTimeout = 10 * time.Second
	// pidFile has the process ID of the spot-go that runs in the
	// background, so the next run of spot-go can stop it.
	pidFile = filepath.Join(soloist.Folder, "background.pid")
	// logFile has what it writes, such as why a media key failed.
	logFile = filepath.Join(soloist.Folder, "background.log")
	// programFolder has the copy of spot-go that runs in the background.
	programFolder = filepath.Join(soloist.Folder, Command)
)

// Start starts spot-go again, in the background, to keep Now Playing and the
// media keys on what the Soloist soloistPid plays, as spot-go quits and
// leaves it playing. device is that Soloist's device ID.
func Start(soloistPid int, device string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("fail to find spot-go: %w", err)
	}
	p, err := start(keep(exe), Args(soloistPid, device))
	if err != nil {
		return err
	}
	p.Release()
	return nil
}

// keep returns the program spot-go in the background runs: exe, spot-go
// itself, or a copy of it when `go run` built it. go run removes the program
// it built once spot-go quits, and macOS then no longer shows in Now Playing,
// nor sends the media keys to, the program that still runs from it. Without
// a copy, spot-go in the background runs exe. A copy that an earlier go run
// left goes away once spot-go runs from elsewhere, as it takes some 20 MB.
func keep(exe string) string {
	if !builtByGoRun(exe) {
		if filepath.Dir(exe) != programFolder {
			_ = os.RemoveAll(programFolder)
		}
		return exe
	}
	kept, err := copyProgram(exe, programFolder)
	if err != nil {
		return exe
	}
	return kept
}

// builtByGoRun reports whether `go run` built exe: it builds a program in a
// folder of its own, named go-build and some digits, in the temporary
// folder.
func builtByGoRun(exe string) bool {
	for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
		if strings.HasPrefix(filepath.Base(dir), "go-build") {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// copyProgram copies the program at exe to folder, as spot-go. It replaces
// an earlier copy at once, so a spot-go that still runs from that one goes
// on, and the next one runs the new copy.
func copyProgram(exe, folder string) (string, error) {
	in, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if err := os.MkdirAll(folder, 0700); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(folder, ".spot-go-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(out.Name())

	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(0700)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	kept := filepath.Join(folder, "spot-go")
	if err := os.Rename(out.Name(), kept); err != nil {
		return "", err
	}
	return kept, nil
}

// Args are the arguments of spot-go in the background.
func Args(soloistPid int, device string) []string {
	return []string{Command, "--" + SoloistFlag, strconv.Itoa(soloistPid), "--" + DeviceFlag, device}
}

func start(name string, args []string) (*command.Process, error) {
	// Another run of spot-go may have left one, as when two ran at once:
	// once its pid file names this one, nothing could stop it.
	Stop()

	// Its output goes to the log file, in a session of its own, so it
	// outlives spot-go and the terminal.
	p, err := command.Start(name, args, logFile, nil)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(p.Pid())+"\n"), 0600); err != nil {
		// The next run of spot-go couldn't stop it, and it would answer the
		// media keys along with that run.
		p.Release()
		_ = p.Kill()
		return nil, fmt.Errorf("fail to save the process ID of spot-go in the background: %w", err)
	}
	return p, nil
}

// Stop stops the spot-go an earlier run left in the background, so it
// neither answers the media keys nor refreshes the Spotify token along with
// this run. It returns the process ID of the Soloist that one followed, or 0
// when none ran.
func Stop() int {
	pid, ok := pidInFile()
	if !ok {
		return 0
	}
	defer os.Remove(pidFile)
	soloistPid, ok := followed(pid)
	if !ok {
		return 0
	}

	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(stopTimeout)
	for command.Running(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			break
		}
		time.Sleep(stopEvery)
	}
	return soloistPid
}

func pidInFile() (int, bool) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil
}

// followed is the process ID of the Soloist pid follows, when pid is spot-go
// in the background, and not a program that got its process ID since.
func followed(pid int) (int, bool) {
	if !command.Running(pid) {
		return 0, false
	}
	line, ok := command.Line(pid)
	if !ok {
		return 0, false
	}
	_, rest, ok := strings.Cut(line, " "+Command+" --"+SoloistFlag+" ")
	if !ok {
		return 0, false
	}
	soloistPid, _ := strconv.Atoi(strings.Fields(rest + " ")[0])
	return soloistPid, true
}

// Run keeps Now Playing and the media keys on what Spotify plays, while the
// Soloist soloistPid, which an earlier run of spot-go left playing, runs, and
// until ctx ends. device is that Soloist's device ID.
func Run(ctx context.Context, soloistPid int, device string) error {
	defer forget()

	running := func() bool { return soloist.StillLeft(soloistPid) }
	if !running() {
		return nil
	}

	commands := make(chan media.Command, commandQueue)
	media.Listen(func(c media.Command) {
		select {
		case commands <- c:
		default:
			// Presses that come faster than Spotify takes them are dropped.
		}
	})
	// Without the media controls, as over SSH on Linux, there is nothing to
	// do.
	lost, err := media.Connected(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-lost:
			log.Print("the media controls went away, as when you log out")
			cancel()
		case <-ctx.Done():
		}
	}()

	if err := config.Load(); err != nil {
		return err
	}
	// A token refresh doesn't end with ctx, as when the next run of spot-go
	// stops this one: Spotify may have given a new token, which only the
	// end of the refresh saves. It can't take forever either, as nothing
	// else runs meanwhile.
	resp := spotify.Login(context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: refreshTimeout}))
	if resp.Err != nil {
		if errors.Is(resp.Err, spotify.ErrNeedsAuth) {
			return errLoggedOut
		}
		return resp.Err
	}
	return newSession(resp.Client, libspotify.ID(device), media.Publish, running).run(ctx, commands)
}

// forget removes the pid file when it still names this process.
func forget() {
	if pid, ok := pidInFile(); ok && pid == os.Getpid() {
		_ = os.Remove(pidFile)
	}
}
