package process

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spot-go/spot-go/internal/command"
)

const (
	tailSize    = 20
	stopTimeout = 5 * time.Second
)

var ErrStopped = errors.New("spotifyd was stopped")

type Process struct {
	cmd           *command.Process
	onLine        func(string)
	authenticated chan struct{}
	failed        chan struct{}
	exited        chan struct{}
	authOnce      sync.Once
	failOnce      sync.Once

	mu      sync.Mutex
	failure error
	exitErr error
	stopped bool
	tail    []string
}

func Start(name string, args []string, onLine func(line string)) (*Process, error) {
	p := &Process{
		onLine:        onLine,
		authenticated: make(chan struct{}),
		failed:        make(chan struct{}),
		exited:        make(chan struct{}),
	}

	cmd, err := command.Start(name, args, p.handle)
	if err != nil {
		return nil, err
	}
	p.cmd = cmd

	go p.wait()
	return p, nil
}

func (p *Process) WaitAuthenticated(timeout time.Duration) error {
	var timeoutC <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timeoutC = t.C
	}

	var err error
	select {
	case <-p.authenticated:
	case <-p.failed:
		err = p.failureErr()
	case <-p.exited:
		err = p.unexpectedExitErr()
	case <-timeoutC:
		err = fmt.Errorf("timed out after %s waiting for spotifyd to log in%s", timeout, p.tailText())
	}

	if p.isStopped() {
		return ErrStopped
	}
	return err
}

func (p *Process) Err() error {
	if p.isStopped() {
		return ErrStopped
	}

	select {
	case <-p.failed:
		return p.failureErr()
	case <-p.exited:
		return p.unexpectedExitErr()
	default:
		return nil
	}
}

func (p *Process) Wait() error {
	<-p.exited

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return ErrStopped
	}
	if p.exitErr != nil {
		return fmt.Errorf("spotifyd failed: %v%s", p.exitErr, tailText(p.tail))
	}
	return nil
}

func (p *Process) Stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	select {
	case <-p.exited:
		return
	default:
	}

	_ = p.cmd.Kill()
	select {
	case <-p.exited:
	case <-time.After(stopTimeout):
	}
}

func (p *Process) wait() {
	err := p.cmd.Wait()

	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()

	close(p.exited)
}

func (p *Process) handle(line string) {
	p.mu.Lock()
	p.tail = append(p.tail, line)
	if len(p.tail) > tailSize {
		p.tail = p.tail[len(p.tail)-tailSize:]
	}
	p.mu.Unlock()

	if p.onLine != nil {
		p.onLine(line)
	}

	switch Classify(line) {
	case Authenticated:
		p.authOnce.Do(func() { close(p.authenticated) })
	case Failed:
		p.failOnce.Do(func() {
			p.mu.Lock()
			p.failure = errors.New(Message(line))
			p.mu.Unlock()
			close(p.failed)
		})
	}
}

func (p *Process) failureErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure
}

func (p *Process) unexpectedExitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exitErr != nil {
		return fmt.Errorf("spotifyd exited: %v%s", p.exitErr, tailText(p.tail))
	}
	return fmt.Errorf("spotifyd exited unexpectedly%s", tailText(p.tail))
}

func (p *Process) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func (p *Process) tailText() string {
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
