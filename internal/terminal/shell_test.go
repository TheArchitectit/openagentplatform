package terminal

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func shellCommand(ctx context.Context, script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", script)
	}
	return exec.CommandContext(ctx, "sh", "-c", script)
}

func TestRemoteShellCreation(t *testing.T) {
	shell := NewRemoteShell(func(ctx context.Context) *exec.Cmd {
		return shellCommand(ctx, "exit 0")
	})
	if _, err := shell.Stdin(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Stdin() error = %v, want %v", err, ErrNotStarted)
	}
	if err := shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := shell.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start() error = %v, want %v", err, ErrAlreadyStarted)
	}
	if err := shell.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if err := shell.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRemoteShellIO(t *testing.T) {
	shell := NewRemoteShell(func(ctx context.Context) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return shellCommand(ctx, "set /p line= & echo got:%line% & echo problem 1>&2")
		}
		return shellCommand(ctx, "IFS= read -r line; printf 'got:%s\\n' \"$line\"; printf 'problem\\n' >&2")
	})
	if err := shell.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	stdin, err := shell.Stdin()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := shell.Stdout()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := shell.Stderr()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, "hello\n"); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutData, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	stderrData, err := io.ReadAll(stderr)
	if err != nil {
		t.Fatal(err)
	}
	if err := shell.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdoutData), "got:hello") {
		t.Fatalf("stdout = %q", stdoutData)
	}
	if !strings.Contains(string(stderrData), "problem") {
		t.Fatalf("stderr = %q", stderrData)
	}
}

func TestRemoteShellCleanup(t *testing.T) {
	shell := NewRemoteShell(func(ctx context.Context) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return shellCommand(ctx, "ping -n 31 127.0.0.1 >NUL")
		}
		return shellCommand(ctx, "sleep 30")
	})
	if err := shell.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- shell.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close() did not terminate the shell")
	}
	if err := shell.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := shell.Stdout(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Stdout() after Close error = %v, want %v", err, ErrClosed)
	}
}
