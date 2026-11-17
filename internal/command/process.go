package command

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// followEvery is how often a process's log is read for new output once
	// it has no more.
	followEvery = 50 * time.Millisecond
	// aliveEvery is how often Wait checks whether an attached process still
	// runs, as it isn't a child spot-go can wait for.
	aliveEvery  = 200 * time.Millisecond
	readBufSize = 32 << 10
)

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

	ErrNotRunning = errors.New("the process is not running")
)

type (
	// Process is a process that writes its output to a log file, in a
	// session of its own, so it keeps running when spot-go exits, and when
	// the terminal spot-go runs in closes.
	Process struct {
		pid int
		// cmd is nil for a process an earlier run of spot-go started.
		cmd      *exec.Cmd
		output   *follower
		released chan struct{}
		release  sync.Once
	}

	// follower reads a log file as a process writes it, and hands each line
	// to a lineWriter.
	follower struct {
		file   *os.File
		lines  *lineWriter
		exited chan struct{}
		quit   chan struct{}
		done   chan struct{}
		exit   sync.Once
		stop   sync.Once
	}

	lineWriter struct {
		mu     sync.Mutex
		buf    []byte
		onLine func(string)
	}
)

// Start starts name with args, its output going to logFile, which it empties
// first. onLine gets each line of the output as name writes it. name runs in
// the folder of logFile, so it doesn't keep the folder spot-go started in
// busy after spot-go quits.
//
// stdout and stderr share logFile, as in a terminal: a line one of them
// writes in several parts can take in a line the other writes meanwhile.
func Start(name string, args []string, logFile string, onLine func(line string)) (*Process, error) {
	out, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("fail to create %s: %w", logFile, err)
	}
	// The process has its own copy of the file once it starts.
	defer out.Close()
	in, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("fail to read %s: %w", logFile, err)
	}

	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Dir = filepath.Dir(logFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		in.Close()
		return nil, fmt.Errorf("fail to start %s: %w", name, err)
	}

	return &Process{
		pid:      cmd.Process.Pid,
		cmd:      cmd,
		output:   follow(in, &lineWriter{onLine: onLine}),
		released: make(chan struct{}),
	}, nil
}

// Attach follows pid, a process Start started in an earlier run of spot-go,
// whose output goes to logFile. onPast gets the lines already in logFile
// before Attach returns, and onLine each line after them.
func Attach(pid int, logFile string, onPast, onLine func(line string)) (*Process, error) {
	if !Running(pid) {
		return nil, ErrNotRunning
	}
	in, err := os.Open(logFile)
	if err != nil {
		return nil, fmt.Errorf("fail to read %s: %w", logFile, err)
	}

	lines := &lineWriter{onLine: onPast}
	buf := make([]byte, readBufSize)
	for {
		n, _ := in.Read(buf)
		if n <= 0 {
			break
		}
		_, _ = lines.Write(buf[:n])
	}
	// A line still being written when Attach read the file is a new one.
	lines.setOnLine(onLine)

	return &Process{
		pid:      pid,
		output:   follow(in, lines),
		released: make(chan struct{}),
	}, nil
}

// Running reports whether pid is a process that runs as this user.
func Running(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func (p *Process) Pid() int {
	return p.pid
}

// Wait waits for the process to exit, and for onLine to get the last of its
// output. A released process doesn't hand out its last lines, and Wait on a
// released attached process returns right away, while it still runs.
func (p *Process) Wait() error {
	var err error
	if p.cmd != nil {
		err = p.cmd.Wait()
	} else {
		p.waitAttached()
	}
	p.output.finish()
	return err
}

func (p *Process) waitAttached() {
	t := time.NewTicker(aliveEvery)
	defer t.Stop()

	for Running(p.pid) {
		select {
		case <-t.C:
		case <-p.released:
			// spot-go no longer follows the process, so it doesn't wait for it
			// either.
			return
		}
	}
}

// Release stops following the process, which keeps running.
func (p *Process) Release() {
	p.release.Do(func() { close(p.released) })
	p.output.abandon()
}

func (p *Process) Kill() error {
	return syscall.Kill(-p.pid, syscall.SIGKILL)
}

func (p *Process) Terminate() error {
	return syscall.Kill(-p.pid, syscall.SIGTERM)
}

func follow(file *os.File, lines *lineWriter) *follower {
	f := &follower{
		file:   file,
		lines:  lines,
		exited: make(chan struct{}),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go f.run()
	return f
}

func (f *follower) run() {
	defer close(f.done)
	defer f.file.Close()

	buf := make([]byte, readBufSize)
	t := time.NewTicker(followEvery)
	defer t.Stop()
	for {
		select {
		case <-f.quit:
			return
		default:
		}
		if f.read(buf) {
			continue
		}

		select {
		case <-f.quit:
			return
		case <-f.exited:
			for f.read(buf) {
			}
			f.lines.flush()
			return
		case <-t.C:
		}
	}
}

// read hands what the file got since the last read to the lines, and
// reports whether there was anything.
func (f *follower) read(buf []byte) bool {
	n, _ := f.file.Read(buf)
	if n <= 0 {
		return false
	}
	_, _ = f.lines.Write(buf[:n])
	return true
}

// finish hands out the rest of the output once the process exited, and its
// last line even without a line break.
func (f *follower) finish() {
	f.exit.Do(func() { close(f.exited) })
	<-f.done
}

// abandon stops reading the output.
func (f *follower) abandon() {
	f.stop.Do(func() { close(f.quit) })
	<-f.done
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) setOnLine(onLine func(string)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.onLine = onLine
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *lineWriter) emit(b []byte) {
	line := strings.TrimRight(ansiEscape.ReplaceAllString(string(b), ""), "\r")
	if strings.TrimSpace(line) != "" && w.onLine != nil {
		w.onLine(line)
	}
}
