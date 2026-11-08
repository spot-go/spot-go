package soloist

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/command"
)

const (
	tailSize        = 20
	stopTimeout     = 5 * time.Second
	killTimeout     = time.Second
	expiredExitCode = 10
)

type process struct {
	cmd       *command.Process
	loggedIn  chan struct{}
	waiting   chan struct{}
	failed    chan struct{}
	exited    chan struct{}
	loginOnce sync.Once
	waitOnce  sync.Once
	failOnce  sync.Once

	mu      sync.Mutex
	id      string
	failure error
	exitErr error
	stopped bool
	tail    []string
}

func startProcess(bin string, args []string) (*process, error) {
	p := &process{
		loggedIn: make(chan struct{}),
		waiting:  make(chan struct{}),
		failed:   make(chan struct{}),
		exited:   make(chan struct{}),
	}

	cmd, err := command.Start(bin, args, p.handle)
	if err != nil {
		return nil, err
	}
	p.cmd = cmd

	go p.wait()
	return p, nil
}

func (p *process) waitLoggedIn(timeout time.Duration, onPairing func()) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	timeoutC := timer.C
	waiting := p.waiting
	for {
		var err error
		select {
		case <-p.loggedIn:
		case <-p.failed:
			err = p.failureErr()
		case <-p.exited:
			err = p.exitError()
		case <-waiting:
			waiting = nil
			timeoutC = nil
			if onPairing != nil {
				onPairing()
			}
			continue
		case <-timeoutC:
			err = fmt.Errorf("timed out after %s waiting for Soloist to log in%s", timeout, p.tailText())
		}

		if p.isStopped() {
			return ErrStopped
		}
		return err
	}
}

func (p *process) err() error {
	if p.isStopped() {
		return ErrStopped
	}

	select {
	case <-p.failed:
		return p.failureErr()
	case <-p.exited:
		return p.exitError()
	default:
		return nil
	}
}

func (p *process) deviceID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

func (p *process) stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	select {
	case <-p.exited:
		return
	default:
	}

	_ = p.cmd.Terminate()
	select {
	case <-p.exited:
		return
	case <-time.After(stopTimeout):
	}

	_ = p.cmd.Kill()
	select {
	case <-p.exited:
	case <-time.After(killTimeout):
	}
}

func (p *process) wait() {
	err := p.cmd.Wait()

	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()

	close(p.exited)
}

func (p *process) handle(line string) {
	p.mu.Lock()
	p.tail = append(p.tail, line)
	if len(p.tail) > tailSize {
		p.tail = p.tail[len(p.tail)-tailSize:]
	}
	p.mu.Unlock()

	switch classify(line) {
	case ready:
		if id, ok := deviceID(line); ok {
			p.mu.Lock()
			p.id = id
			p.mu.Unlock()
		}
	case waitingForLogin:
		p.waitOnce.Do(func() { close(p.waiting) })
	case loggedIn:
		p.loginOnce.Do(func() { close(p.loggedIn) })
	case loginFailed:
		p.fail(ErrInvalidAPIKey)
	case failed:
		p.fail(errors.New(message(line)))
	}
}

func (p *process) fail(err error) {
	p.failOnce.Do(func() {
		p.mu.Lock()
		p.failure = err
		p.mu.Unlock()
		close(p.failed)
	})
}

func (p *process) failureErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure
}

func (p *process) exitError() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var exitErr *exec.ExitError
	if errors.As(p.exitErr, &exitErr) && exitErr.ExitCode() == expiredExitCode {
		return ErrExpired
	}
	if p.exitErr != nil {
		return fmt.Errorf("Soloist exited: %v%s", p.exitErr, tailText(p.tail))
	}
	return fmt.Errorf("Soloist exited unexpectedly%s", tailText(p.tail))
}

func (p *process) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func (p *process) tailText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return tailText(p.tail)
}

func tailText(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}
