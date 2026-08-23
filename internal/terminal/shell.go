// Package terminal manages local terminal processes used by remote sessions.
package terminal

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

var (
	ErrAlreadyStarted = errors.New("terminal: shell already started")
	ErrNotStarted     = errors.New("terminal: shell not started")
	ErrClosed         = errors.New("terminal: shell closed")
)

// CommandFactory builds the process used by a remote shell.
type CommandFactory func(ctx context.Context) *exec.Cmd

// RemoteShell manages the lifecycle and I/O pipes of a terminal process.
// The standard library has no portable PTY API, so the process pipes form the
// transport boundary and can be attached to a platform PTY by a caller.
type RemoteShell struct {
	mu      sync.Mutex
	factory CommandFactory
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	stdoutW io.WriteCloser
	stderrW io.WriteCloser
	started bool
	closed  bool
	waitErr error
	waitCh  chan struct{}
}

// NewRemoteShell constructs a shell using factory.
func NewRemoteShell(factory CommandFactory) *RemoteShell {
	return &RemoteShell{factory: factory, waitCh: make(chan struct{})}
}

// Start allocates process pipes and starts the terminal process.
func (s *RemoteShell) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.started {
		return ErrAlreadyStarted
	}
	if s.factory == nil {
		return errors.New("terminal: command factory required")
	}
	cmd := s.factory(ctx)
	if cmd == nil {
		return errors.New("terminal: command factory returned nil")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, stdoutW := io.Pipe()
	stderr, stderrW := io.Pipe()
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutW.Close()
		_ = stderr.Close()
		_ = stderrW.Close()
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout
	s.stderr = stderr
	s.stdoutW = stdoutW
	s.stderrW = stderrW
	s.started = true
	go s.wait(cmd)
	return nil
}

func (s *RemoteShell) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	s.mu.Lock()
	s.waitErr = err
	stdoutW, stderrW := s.stdoutW, s.stderrW
	s.mu.Unlock()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	close(s.waitCh)
}

// Stdin returns the input stream for the terminal process.
func (s *RemoteShell) Stdin() (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, ErrNotStarted
	}
	if s.closed {
		return nil, ErrClosed
	}
	return s.stdin, nil
}

// Stdout returns the output stream for the terminal process.
func (s *RemoteShell) Stdout() (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, ErrNotStarted
	}
	if s.closed {
		return nil, ErrClosed
	}
	return s.stdout, nil
}

// Stderr returns the error stream for the terminal process.
func (s *RemoteShell) Stderr() (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, ErrNotStarted
	}
	if s.closed {
		return nil, ErrClosed
	}
	return s.stderr, nil
}

// Wait blocks until the terminal process exits.
func (s *RemoteShell) Wait() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return ErrNotStarted
	}
	waitCh := s.waitCh
	s.mu.Unlock()
	<-waitCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

// Close releases the shell pipes and terminates a running process.
func (s *RemoteShell) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	stdin, stdout, stderr := s.stdin, s.stdout, s.stderr
	stdoutW, stderrW := s.stdoutW, s.stderrW
	process := s.cmd.Process
	waitCh := s.waitCh
	s.mu.Unlock()

	_ = stdin.Close()
	if process != nil {
		_ = process.Kill()
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()
	_ = stdout.Close()
	_ = stderr.Close()
	<-waitCh
	return nil
}
