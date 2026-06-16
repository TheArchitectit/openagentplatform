package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// ScriptCommand arrives on the agent's scripts subject.
type ScriptCommand struct {
	ScriptID  string            `json:"script_id"`
	Runtime   string            `json:"runtime"`            // bash, sh, powershell, pwsh, python, node, cmd
	Script    string            `json:"script"`             // inline source
	URL       string            `json:"url,omitempty"`      // optional URL to download
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutSec int              `json:"timeout_sec,omitempty"`
}

// ScriptOutputChunk is streamed to the result subject.
type ScriptOutputChunk struct {
	ScriptID  string `json:"script_id"`
	AgentID   string `json:"agent_id"`
	Stream    string `json:"stream"` // "stdout" | "stderr" | "exit" | "error"
	Data      string `json:"data,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ScriptsSubject returns the NATS subject for incoming script commands.
func ScriptsSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts", agentID)
}

// ScriptsResultSubject returns the NATS subject for streamed script output.
func ScriptsResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts.result", agentID)
}

// RunScriptsHandler subscribes to the scripts subject and executes each
// request, streaming stdout/stderr to the result subject.
func RunScriptsHandler(ctx context.Context, agentID string, defaultTimeoutSec int, nc *NATSClient, log *slog.Logger) (*nats.Subscription, error) {
	subject := ScriptsSubject(agentID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var cmd ScriptCommand
		if err := json.Unmarshal(msg.Data, &cmd); err != nil {
			log.Warn("scripts: bad payload", "err", err, "subject", subject)
			return
		}
		if cmd.ScriptID == "" {
			cmd.ScriptID = uuid.NewString()
		}
		log.Info("script received", "script_id", cmd.ScriptID, "runtime", cmd.Runtime)
		runScript(ctx, agentID, &cmd, defaultTimeoutSec, nc, log)
	})
	if err != nil {
		return nil, err
	}
	log.Info("scripts handler subscribed", "subject", subject)
	return sub, nil
}

func runScript(parent context.Context, agentID string, cmd *ScriptCommand, defaultTimeout int, nc *NATSClient, log *slog.Logger) {
	resultSubject := ScriptsResultSubject(agentID)
	publish := func(chunk ScriptOutputChunk) {
		if chunk.Timestamp == 0 {
			chunk.Timestamp = time.Now().Unix()
		}
		chunk.AgentID = agentID
		chunk.ScriptID = cmd.ScriptID
		data, err := json.Marshal(chunk)
		if err != nil {
			log.Warn("script chunk marshal failed", "err", err)
			return
		}
		if err := nc.Publish(parent, resultSubject, data); err != nil {
			log.Warn("script chunk publish failed", "err", err)
		}
	}

	runtimeName := strings.ToLower(strings.TrimSpace(cmd.Runtime))
	if runtimeName == "" {
		runtimeName = detectRuntime(cmd.Script, cmd.URL)
	}

	timeoutSec := cmd.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeout
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	execCmd, err := buildScriptCommand(ctx, runtimeName, cmd)
	if err != nil {
		publish(ScriptOutputChunk{Stream: "error", Data: err.Error()})
		publish(ScriptOutputChunk{Stream: "exit", ExitCode: -1})
		return
	}

	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		publish(ScriptOutputChunk{Stream: "error", Data: "stdout pipe: " + err.Error()})
		return
	}
	stderr, err := execCmd.StderrPipe()
	if err != nil {
		publish(ScriptOutputChunk{Stream: "error", Data: "stderr pipe: " + err.Error()})
		return
	}

	if err := execCmd.Start(); err != nil {
		publish(ScriptOutputChunk{Stream: "error", Data: "start: " + err.Error()})
		publish(ScriptOutputChunk{Stream: "exit", ExitCode: -1})
		return
	}

	doneCh := make(chan struct{}, 2)
	go func() { streamLines(stdout, "stdout", publish, log); doneCh <- struct{}{} }()
	go func() { streamLines(stderr, "stderr", publish, log); doneCh <- struct{}{} }()

	waitErr := execCmd.Wait()
	<-doneCh
	<-doneCh

	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	publish(ScriptOutputChunk{Stream: "exit", ExitCode: exitCode})
}

func streamLines(r io.Reader, stream string, publish func(ScriptOutputChunk), log *slog.Logger) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		publish(ScriptOutputChunk{Stream: stream, Data: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		log.Warn("script stream error", "stream", stream, "err", err)
		publish(ScriptOutputChunk{Stream: "error", Data: err.Error()})
	}
}

func buildScriptCommand(ctx context.Context, runtimeName string, cmd *ScriptCommand) (*exec.Cmd, error) {
	var name string
	var args []string

	switch runtimeName {
	case "bash", "sh":
		name = "bash"
		if runtime.GOOS == "windows" {
			// Prefer git-bash or wsl if available; otherwise fall through to powershell
			name = "bash"
		}
		args = append(args, "-c", cmd.Script)
	case "powershell", "pwsh":
		name = "powershell"
		if runtime.GOOS != "windows" {
			name = "pwsh"
		}
		args = append(args, "-NoProfile", "-NonInteractive", "-Command", cmd.Script)
	case "python", "python3", "py":
		name = "python3"
		if runtime.GOOS == "windows" {
			name = "python"
		}
		args = append(args, "-c", cmd.Script)
	case "node", "nodejs", "javascript":
		name = "node"
		args = append(args, "-e", cmd.Script)
	case "cmd", "batch":
		name = "cmd"
		args = append(args, "/C", cmd.Script)
	default:
		return nil, fmt.Errorf("unsupported runtime: %q", cmd.Runtime)
	}

	execCmd := exec.CommandContext(ctx, name, args...)
	execCmd.Args = append(execCmd.Args, cmd.Args...)
	if len(cmd.Env) > 0 {
		env := make([]string, 0, len(cmd.Env))
		for k, v := range cmd.Env {
			env = append(env, k+"="+v)
		}
		execCmd.Env = append(execCmd.Env, env...)
	}
	return execCmd, nil
}

func detectRuntime(script, url string) string {
	lower := strings.ToLower(script + " " + url)
	switch {
	case strings.HasPrefix(lower, "#!/usr/bin/env python"), strings.HasPrefix(lower, "#!/usr/bin/python"), strings.Contains(lower, "import "):
		return "python"
	case strings.HasPrefix(lower, "#!/bin/bash"), strings.HasPrefix(lower, "#!/usr/bin/env bash"):
		return "bash"
	case strings.HasPrefix(lower, "#!/usr/bin/env node"):
		return "node"
	case strings.HasPrefix(lower, "$"), strings.Contains(lower, "get-"):
		return "powershell"
	}
	return "bash"
}
