package command

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

type (
	Process struct {
		cmd    *exec.Cmd
		stdout *lineWriter
		stderr *lineWriter
	}

	lineWriter struct {
		mu     *sync.Mutex
		buf    []byte
		onLine func(string)
	}
)

func Start(name string, args []string, onLine func(line string)) (*Process, error) {
	mu := &sync.Mutex{}
	p := &Process{
		cmd:    exec.Command(name, args...),
		stdout: &lineWriter{mu: mu, onLine: onLine},
		stderr: &lineWriter{mu: mu, onLine: onLine},
	}
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = p.stderr
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("fail to start %s: %w", name, err)
	}

	return p, nil
}

func (p *Process) Wait() error {
	err := p.cmd.Wait()
	p.stdout.flush()
	p.stderr.flush()
	return err
}

func (p *Process) Kill() error {
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

func (p *Process) Terminate() error {
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
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
