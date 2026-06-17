package inject

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// stdinInjector delivers secrets via a one-shot unix socket or named pipe.
type stdinInjector struct {
	mu      sync.Mutex
	pipes   map[string]*oneShotPipe // spec.Key -> pipe
}

// newStdinInjector creates a stdinInjector.
func newStdinInjector() *stdinInjector {
	return &stdinInjector{pipes: make(map[string]*oneShotPipe)}
}

// oneShotPipe is a unix-domain socket that serves the credential bytes to the
// first reader, then closes itself.
type oneShotPipe struct {
	path string
	conn net.Listener
	data []byte
	done chan struct{}
}

// inject creates a one-shot unix socket containing the credential. The agent
// process connects to the returned path, reads the bytes, and the pipe
// self-destructs after the first successful read.
func (s *stdinInjector) inject(spec InjectionSpec) (string, error) {
	dir := os.TempDir()
	if dir == "" {
		dir = "/tmp"
	}

	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("stdin: random: %w", err)
	}
	token := hex.EncodeToString(randBytes)

	agentID := sanitizeKey(spec.AgentID())
	if agentID == "" {
		agentID = "agent"
	}

	sockPath := filepath.Join(dir, fmt.Sprintf("oap-stdin-%s-%s.sock", agentID, token))

	// Remove any stale socket file.
	_ = os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return "", fmt.Errorf("stdin: listen: %w", err)
	}

	if err := os.Chmod(sockPath, 0o600); err != nil {
		listener.Close()
		os.Remove(sockPath)
		return "", fmt.Errorf("stdin: chmod: %w", err)
	}

	pipe := &oneShotPipe{
		path: sockPath,
		conn: listener,
		data: spec.Value,
		done: make(chan struct{}),
	}

	// Serve the credential to the first connection, then close.
	go func() {
		defer close(pipe.done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write(pipe.data)
	}()

	s.mu.Lock()
	s.pipes[spec.Key] = pipe
	s.mu.Unlock()

	return sockPath, nil
}

// cleanup removes the unix socket and zeros the held data.
func (s *stdinInjector) cleanup(spec InjectionSpec) error {
	s.mu.Lock()
	pipe, ok := s.pipes[spec.Key]
	delete(s.pipes, spec.Key)
	s.mu.Unlock()

	if !ok {
		return nil
	}

	if pipe.conn != nil {
		pipe.conn.Close()
	}

	// Zero the in-memory copy.
	for i := range pipe.data {
		pipe.data[i] = 0
	}

	// Wait briefly for the serve goroutine to finish, but don't block forever.
	select {
	case <-pipe.done:
	case <-pipeOrTimeout():
	}

	return os.Remove(pipe.path)
}

// pipeOrTimeout returns a channel that fires after a short grace period.
func pipeOrTimeout() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		<-ctx.Done()
		close(ch)
	}()
	return ch
}

// contextWithTimeout creates a context with a 2-second deadline.
func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), shortTimeout)
}

// shortTimeout is the grace period for stdin pipe cleanup.
const shortTimeout = 2_000_000_000 // 2 seconds in nanoseconds
