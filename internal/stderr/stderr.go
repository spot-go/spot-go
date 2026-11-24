// Package stderr keeps what goes to standard error off the terminal while
// spot-go runs there.
//
// On macOS, once memory runs critically low, the system loads its malloc
// stack logging library into the programs that run, to report on their
// memory, without turning the logging on. From then on, every process such a
// program forks writes
//
//	spot-go(PID) MallocStackLogging: can't turn off malloc stack logging because it was not enabled.
//
// to the standard error it got from the program, as it starts, before it
// runs the command spot-go started: hence spot-go's name, with the process
// ID of the command. spot-go starts commands as it runs, such as ps, pkill
// and Soloist's ctl, and, as it quits leaving the music playing, ps and
// spot-go in the background. On the terminal, their lines would garble the
// screen spot-go draws, and the lines of the commands it starts as it quits,
// once its screen closed, would stay there.
package stderr

import (
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// noise is what the processes spot-go starts write on macOS.
const noise = "MallocStackLogging: can't turn off malloc stack logging because it was not enabled."

var (
	// redirects reports whether standard error goes to a file while spot-go
	// runs: only on macOS, where the processes spot-go starts write noise.
	redirects = runtime.GOOS == "darwin"

	isTerminal = term.IsTerminal
)

// Redirect is standard error going to a file instead of the terminal.
type Redirect struct {
	// terminal is the terminal standard error was, and fd its file
	// descriptor.
	terminal *os.File
	fd       int
	file     *os.File
	// mark is where the file ended when Mark was called.
	mark int64
}

// ToFile points standard error at the file name, when standard error is a
// terminal, on macOS. What spot-go writes there goes to the file, and so does
// what the processes it starts write before they run their command. A crash
// still shows on the terminal too, but for its first line, such as "fatal
// error: concurrent map writes", and what a library writes before it aborts,
// which only go to the file. So the file of the last run stays, as name with
// .old before its extension. ToFile returns nil when standard error stays as
// it is.
func ToFile(name string) *Redirect {
	if !redirects || !isTerminal(uintptr(unix.Stderr)) {
		return nil
	}
	// The runtime then waits on standard error through its poller, which
	// loses it once another file takes its place: once back, a write that
	// has to wait would wait forever.
	if flags, err := unix.FcntlInt(uintptr(unix.Stderr), unix.F_GETFL, 0); err != nil || flags&unix.O_NONBLOCK != 0 {
		return nil
	}

	// A new file also keeps the lines of another run of spot-go, which
	// writes to the old one, out of this run's.
	_ = os.Rename(name, oldName(name))
	file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return nil
	}
	// Close-on-exec, so the commands spot-go starts, such as Soloist, which
	// can outlive it, don't keep the terminal open.
	fd, err := unix.FcntlInt(uintptr(unix.Stderr), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		file.Close()
		return nil
	}
	terminal := os.NewFile(uintptr(fd), "/dev/stderr")
	if err := unix.Dup2(int(file.Fd()), unix.Stderr); err != nil {
		terminal.Close()
		file.Close()
		return nil
	}
	// The runtime writes a crash to standard error, and, as it did before,
	// to the terminal, where it shows over the screen spot-go draws.
	_ = debug.SetCrashOutput(terminal, debug.CrashOptions{})
	return &Redirect{terminal: terminal, fd: fd, file: file}
}

func oldName(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + ".old" + ext
}

// Mark notes where the file ends, as spot-go's screen closes: before, what
// went to standard error showed on the screen, and went away with it. Mark
// does nothing on a nil Redirect.
func (r *Redirect) Mark() {
	if r == nil {
		return
	}
	if info, err := r.file.Stat(); err == nil {
		r.mark = info.Size()
	}
}

// Restore points standard error back at the terminal, and writes there what
// went to the file since Mark, or all of it with all, such as the panic
// spot-go recovered from, but the noise. Restore does nothing on a nil
// Redirect.
func (r *Redirect) Restore(all bool) {
	if r == nil {
		return
	}
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	_ = unix.Dup2(r.fd, unix.Stderr)
	from := r.mark
	if all {
		from = 0
	}
	// Read from there, not from where the file is at, which the writes left
	// at its end.
	if b, err := io.ReadAll(io.NewSectionReader(r.file, from, math.MaxInt64-from)); err == nil {
		_, _ = r.terminal.Write(withoutNoise(b))
	}
	r.terminal.Close()
	r.file.Close()
}

// withoutNoise is b without its lines of noise.
func withoutNoise(b []byte) []byte {
	var kept []byte
	for line := range bytes.Lines(b) {
		if !bytes.Contains(line, []byte(noise)) {
			kept = append(kept, line...)
		}
	}
	return kept
}
