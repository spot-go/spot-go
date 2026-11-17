// Package background keeps Now Playing and the media keys on the music
// Soloist plays once spot-go quits and leaves it playing. spot-go starts
// itself again for that, in the background, and the next run of spot-go
// stops it.
package background

import (
	"context"
	"errors"
	"fmt"
	"log"
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
)

const (
	// Command is the hidden spot-go command that runs in the background.
	Command = "background"
	// SoloistFlag and DeviceFlag are its flags: the process ID of the
	// Soloist left playing, and its device ID.
	SoloistFlag = "soloist"
	DeviceFlag  = "device"

	stopEvery = 20 * time.Millisecond
)

var (
	// stopTimeout is how long Stop waits for spot-go in the background to
	// end before it kills it.
	stopTimeout = 2 * time.Second
	// pidFile has the process ID of the spot-go that runs in the
	// background, so the next run of spot-go can stop it.
	pidFile = filepath.Join(soloist.Folder, "background.pid")
	// logFile has what it writes, such as why a media key failed.
	logFile = filepath.Join(soloist.Folder, "background.log")
)

// Start starts spot-go again, in the background, to keep Now Playing and the
// media keys on what the Soloist soloistPid plays, as spot-go quits and
// leaves it playing. device is that Soloist's device ID.
func Start(soloistPid int, device string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("fail to find spot-go: %w", err)
	}
	p, err := start(exe, Args(soloistPid, device))
	if err != nil {
		return err
	}
	p.Release()
	return nil
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
// this run.
func Stop() {
	pid, ok := pidInFile()
	if !ok {
		return
	}
	defer os.Remove(pidFile)
	if !runsInBackground(pid) {
		return
	}

	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(stopTimeout)
	for command.Running(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			return
		}
		time.Sleep(stopEvery)
	}
}

func pidInFile() (int, bool) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil
}

// runsInBackground reports whether pid is spot-go in the background, and not
// a program that got its process ID since.
func runsInBackground(pid int) bool {
	if !command.Running(pid) {
		return false
	}
	line, ok := command.Line(pid)
	return ok && strings.Contains(line, " "+Command+" --"+SoloistFlag+" ")
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
	// end of the refresh saves.
	resp := spotify.Login(context.Background())
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
