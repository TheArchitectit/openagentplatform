export const meta = {
  name: 'sprint-15-impl',
  description: 'Implement Sprint 1.5: Script CRUD, 4-runtime executor, Monaco UI, remote shell, session recording',
  phases: [
    { title: 'ScriptCRUD', detail: 'Script library CRUD API' },
    { title: 'ScriptExec', detail: '4-runtime script executor in agent' },
    { title: 'ScriptUI', detail: 'Script UI with Monaco editor' },
    { title: 'RemoteShell', detail: 'SSH/WinRM remote shell (xterm.js + noVNC)' },
    { title: 'SessionRec', detail: 'Remote session audit and recording playback' },
    { title: 'Commit', detail: 'Stage, commit, push, close Sprint 1.5 issues' },
  ],
}

log('Sprint 1.5 implementation starting -- Scripts + Remote Shell')

var repoRoot = '/mnt/data/git/openagentplatform'

// Phase 1: Script library CRUD API (sonnet)
phase('ScriptCRUD')
var scriptCRUD = await agent(
  'Implement the Script library CRUD API at ' + repoRoot + '.\n' +
  'Read existing internal/api/routes.go, pkg/models/models.go, internal/api/check_store.go (for store pattern) first.\n' +
  '\n' +
  '**1. Create internal/api/scripts.go** -- Script CRUD handlers:\n' +
  '- POST /api/v1/scripts -- create script (name, description, runtime [bash/powershell/python/node], script_body, timeout_seconds, enabled, tags[])\n' +
  '- GET /api/v1/scripts -- list scripts (paginated, filter by runtime, enabled, tag, search by name)\n' +
  '- GET /api/v1/scripts/{id} -- single script with run history summary\n' +
  '- PUT /api/v1/scripts/{id} -- update script (name, body, runtime, timeout, enabled, tags)\n' +
  '- DELETE /api/v1/scripts/{id} -- soft-delete\n' +
  '- POST /api/v1/scripts/{id}/run -- enqueue script run on specified agent(s), return run_id(s)\n' +
  '- GET /api/v1/scripts/{id}/runs -- run history for script (paginated, filterable by agent, status)\n' +
  '- GET /api/v1/scripts/runs/{run_id} -- single run detail with full output\n' +
  '\n' +
  'Script run states: pending, running, completed, failed, timed_out, cancelled\n' +
  'Script run stores: run_id, script_id, agent_id, status, started_at, finished_at, exit_code, stdout, stderr, triggered_by, scheduled\n' +
  '\n' +
  '**2. Create internal/api/script_store.go** -- PostgreSQL:\n' +
  '- ScriptStore interface: InsertScript, GetScript, ListScripts (with filters), UpdateScript, DeleteScript\n' +
  '- InsertScriptRun, GetScriptRun, ListScriptRuns (by script, by agent), UpdateScriptRunOutput\n' +
  '- pgScriptStore implementation\n' +
  '\n' +
  '**3. Update internal/api/routes.go** -- register script routes\n' +
  '**4. Update pkg/models/models.go** -- ScriptDefinition, ScriptRun structs\n' +
  '**5. Update internal/api/handler.go** -- add scriptStore setter\n' +
  '**6. Update cmd/server/main.go** -- wire scriptStore\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'script-crud', phase: 'ScriptCRUD', model: 'sonnet' }
)

// Phase 2: 4-runtime script executor in agent (sonnet)
phase('ScriptExec')
var scriptExec = await agent(
  'Enhance the 4-runtime script executor in the agent at ' + repoRoot + '.\n' +
  'Read existing pkg/agent/scripts.go, cmd/agent/main.go first.\n' +
  '\n' +
  '**1. Rewrite pkg/agent/scripts.go** -- Multi-runtime executor:\n' +
  '- Runtime detection from script command or explicit runtime field\n' +
  '- Bash: /bin/bash -c, timeout via timeout command, env vars passed through\n' +
  '- PowerShell: powershell.exe -NoProfile -ExecutionPolicy Bypass -Command (Windows), pwsh -c (Linux)\n' +
  '- Python: python3 -c or python3 script.py (temp file), pip packages can be listed as dependencies\n' +
  '- Node: node -e or node script.js (temp file)\n' +
  '- Temp file management: write script to temp dir, execute, clean up, never leave scripts on disk\n' +
  '- Environment isolation: set HOME, PATH, TEMP; allow env var overrides per script\n' +
  '- Output streaming:\n' +
  '  * stdout/stderr captured separately via os.Pipe or cmd.StdoutPipe/StderrPipe\n' +
  '  * Line-by-line streaming to NATS subject oap.agents.<id>.scripts.<run_id>.output\n' +
  '  * Result published to oap.agents.<id>.scripts.result with exit_code + truncated output\n' +
  '- Timeout enforcement: context.WithTimeout, kill process group on timeout\n' +
  '- Cancellation: subscribe to oap.agents.<id>.scripts.cancel with run_id, kill process\n' +
  '\n' +
  '**2. Create pkg/agent/executor/executor.go** -- Executor abstraction:\n' +
  '- ScriptExecutor interface: Execute(ctx, runtime, script, env, timeout) -> Result\n' +
  '- RuntimeRegistry: map[runtime]ScriptExecutor\n' +
  '- Per-runtime executors: BashExecutor, PowerShellExecutor, PythonExecutor, NodeExecutor\n' +
  '- Cross-platform: detect available runtimes, report which are available\n' +
  '- Sandbox: optional sandbox mode (chroot or restricted user for Linux, AppContainer for Windows -- stub for MVP)\n' +
  '\n' +
  '**3. Update cmd/agent/main.go**:\n' +
  '- Register enhanced script handler\n' +
  '- Subscribe to script run + cancel subjects\n' +
  '- Log available runtimes on startup\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./cmd/agent/ && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'script-executor', phase: 'ScriptExec', model: 'sonnet' }
)

// Phase 3: Script UI with Monaco editor (sonnet -- React)
phase('ScriptUI')
var scriptUI = await agent(
  'Build the Script UI with Monaco editor at ' + repoRoot + '.\n' +
  'Read existing web/src/routes/, web/src/components/, web/src/lib/ first. DO NOT run pnpm/npm install.\n' +
  '\n' +
  '**1. Create web/src/routes/scripts/index.tsx** -- Script library:\n' +
  '- Filter tabs: All, Bash, PowerShell, Python, Node\n' +
  '- Table: Name, Runtime badge, Description, Last Run, Status, Actions\n' +
  '- Click row -> /scripts/$scriptId\n' +
  '- Create Script button -> /scripts/new\n' +
  '\n' +
  '**2. Create web/src/routes/scripts/new.tsx** -- Script editor with Monaco:\n' +
  '- A simple code editor using a textarea with monospace font + line numbers (Monaco is heavy, use a lightweight approach unless Monaco is easily available via CDN, in which case use @monaco-editor/react with CDN loader)\n' +
  '- Script metadata form: name, description, runtime dropdown, timeout, tags\n' +
  '- Runtime-specific syntax highlighting (mode per language)\n' +
  '- Save button: POST /api/v1/scripts\n' +
  '- Test Run button: selects target agent, POST /api/v1/scripts/{id}/run\n' +
  '\n' +
  '**3. Create web/src/routes/scripts/$scriptId.tsx** -- Script detail:\n' +
  '- Script info card: name, description, runtime, timeout, tags, created/updated\n' +
  '- Code viewer (read-only syntax highlighted)\n' +
  '- Edit button -> toggle to editor mode\n' +
  '- Run History table: Run ID, Agent, Started, Finished, Duration, Status, Exit Code\n' +
  '- Click run -> /scripts/$scriptId/runs/$runId\n' +
  '- Run Now button: select agent(s) from dropdown, execute\n' +
  '\n' +
  '**4. Create web/src/routes/scripts/$scriptId/runs/$runId.tsx** -- Run detail:\n' +
  '- Run metadata: script name, agent hostname, status, start/end/duration, exit code, triggered by\n' +
  '- Output viewer: terminal-like black background, monospace, stdout (white) + stderr (red)\n' +
  '- Live output: if run is in_progress, subscribe to WebSocket for streaming output\n' +
  '- Cancel button: if run is in_progress\n' +
  '\n' +
  '**5. Create web/src/lib/useScripts.ts** -- React hook\n' +
  '**6. Update web/src/components/sidebar.tsx** -- ensure Scripts nav link\n' +
  '**7. Update web/src/routes/dashboard.tsx** -- add script run stats\n' +
  '\n' +
  'Report back files created/modified.',
  { label: 'script-ui', phase: 'ScriptUI', model: 'sonnet' }
)

// Phase 4: SSH/WinRM remote shell (sonnet -- xterm.js + noVNC)
phase('RemoteShell')
var remoteShell = await agent(
  'Build the remote shell infrastructure at ' + repoRoot + '.\n' +
  'Read existing internal/api/websocket.go, pkg/agent/ scripts.go, cmd/agent/main.go first.\n' +
  '\n' +
  '**1. Create internal/remote/shell.go** -- Server-side shell proxy:\n' +
  '- ShellSession struct: session_id, agent_id, user_id, protocol (ssh/winrm), terminal_size, started_at\n' +
  '- ShellManager: create, get, list, kill sessions\n' +
  '- Per-session NATS subjects:\n' +
  '  * oap.agents.<id>.shell.<session>.stdin -- user input to agent\n' +
  '  * oap.agents.<id>.shell.<session>.stdout -- agent output to user\n' +
  '  * oap.agents.<id>.shell.<session>.resize -- terminal resize\n' +
  '  * oap.agents.<id>.shell.<session>.close -- close request\n' +
  '- WebSocket proxy: WebSocket messages <-> NATS messages\n' +
  '  * ws message {type: stdin, data: base64} -> NATS stdin\n' +
  '  * NATS stdout -> ws message {type: stdout, data: base64}\n' +
  '  * Resize events -> NATS resize\n' +
  '- Authentication: user JWT required to create shell session\n' +
  '- RBAC: remote:shell permission required\n' +
  '- Max sessions per user (default 10), max sessions per agent (default 5)\n' +
  '- Idle timeout: close session after 30min inactivity\n' +
  '- Rate limiting: max input rate per session\n' +
  '\n' +
  '**2. Create internal/remote/auth.go** -- Remote auth:\n' +
  '- RemoteCredential struct: username, credential_type (password/key/certificate), credential_data (encrypted)\n' +
  '- CredentialStore: encrypt/decrypt credentials with server key\n' +
  '- CredentialResolver: resolve credentials for agent (by agent_id, by site, by org default)\n' +
  '- Temporary credentials: generate one-time-use credentials for session (valid for session duration)\n' +
  '- Auto-rotate credentials on session close\n' +
  '\n' +
  '**3. Create internal/api/remote.go** -- Remote shell API:\n' +
  '- GET /api/v1/shell/sessions -- list active sessions (admin: all, user: own)\n' +
  '- POST /api/v1/agents/{id}/shell -- create shell session (protocol: ssh/winrm, returns session_id + ws_url)\n' +
  '- WS /api/v1/shell/{session_id}/ws -- WebSocket terminal endpoint\n' +
  '- POST /api/v1/shell/{session_id}/kill -- force-kill session\n' +
  '- GET /api/v1/shell/{session_id} -- session status + metadata\n' +
  '- POST /api/v1/credentials -- store credentials (username, type, data, site_id or agent_id)\n' +
  '- GET /api/v1/credentials -- list credentials (masked)\n' +
  '- DELETE /api/v1/credentials/{id}\n' +
  '\n' +
  '**4. Create pkg/agent/shell/shell.go** -- Agent-side shell handler:\n' +
  '- ShellSessionManager on agent\n' +
  '- On shell start: launch ssh/winrm/pty based on protocol\n' +
  '- Pipe NATS stdin -> process stdin, process stdout/stderr -> NATS stdout\n' +
  '- Handle terminal resize (SIGWINCH on Unix, SetConsoleScreenBufferSize on Windows)\n' +
  '- Handle session close: kill process group, cleanup\n' +
  '- Max concurrent shells per agent (default 5)\n' +
  '\n' +
  '**5. Update cmd/agent/main.go** -- register shell handler\n' +
  '**6. Update internal/api/routes.go** -- register remote routes\n' +
  '**7. Create web/src/routes/agents/$agentId/shell.tsx**:\n' +
  '- Terminal emulator using xterm.js (loaded via CDN: https://cdn.jsdelivr.net/npm/@xterm/xterm@5/lib/xterm.js + xterm.css)\n' +
  '- WebSocket connection to /api/v1/shell/{session_id}/ws\n' +
  '- Send keystrokes as stdin messages, render stdout messages in terminal\n' +
  '- Fit addon for auto-resize\n' +
  '- Terminal toolbar: session info, close button, fullscreen toggle\n' +
  '- Disconnect banner with reconnect button\n' +
  '\n' +
  '**8. Update web/src/lib/websocket.ts** -- add shell URL support\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'remote-shell', phase: 'RemoteShell', model: 'sonnet' }
)

// Phase 5: Remote session audit and recording playback (sonnet)
phase('SessionRec')
var sessionRec = await agent(
  'Build remote session audit and recording playback at ' + repoRoot + '.\n' +
  'Read existing internal/remote/shell.go, internal/audit/audit.go, internal/api/audit.go first.\n' +
  '\n' +
  '**1. Create internal/remote/recorder.go** -- Session recorder:\n' +
  '- SessionRecorder wraps a shell session and records all I/O\n' +
  '- Recording format: time-sorted events {timestamp, direction (in/out), data}\n' +
  '- Store in session_recordings table as JSONB chunks (1MB per chunk, paginated)\n' +
  '- Compression: gzip chunks before storage\n' +
  '- Streaming write: buffer 100 events then flush to DB\n' +
  '- Metadata: session_id, agent_id, user_id, protocol, terminal_size, duration, bytes_in, bytes_out\n' +
  '- On session close: finalize recording, compute hash chain entry\n' +
  '\n' +
  '**2. Create internal/remote/recording_store.go** -- PostgreSQL:\n' +
  '- SessionRecordingStore interface\n' +
  '- InsertRecordingChunk, GetRecordingChunks, GetRecordingMetadata\n' +
  '- ListRecordings (filterable by agent, user, time range, session_id search)\n' +
  '- DeleteRecording (retention-based: auto-delete after retention_days)\n' +
  '\n' +
  '**3. Create internal/api/session_audit.go** -- Recording API:\n' +
  '- GET /api/v1/shell/recordings -- list recordings (paginated, filterable)\n' +
  '- GET /api/v1/shell/recordings/{session_id} -- recording metadata + playback info\n' +
  '- GET /api/v1/shell/recordings/{session_id}/play -- playback endpoint (SSE stream of events, with speed control query param)\n' +
  '- DELETE /api/v1/shell/recordings/{session_id} -- hard delete (admin only)\n' +
  '- GET /api/v1/shell/recordings/{session_id}/export -- export as .cast file (asciinema format) for sharing\n' +
  '\n' +
  '**4. Create web/src/routes/shell-recordings/index.tsx** -- Recordings list:\n' +
  '- Table: Session ID, User, Agent, Protocol, Duration, Bytes, Date, Actions\n' +
  '- Click row -> /shell-recordings/$sessionId\n' +
  '- Search by agent hostname or user\n' +
  '- Date range filter\n' +
  '\n' +
  '**5. Create web/src/routes/shell-recordings/$sessionId.tsx** -- Playback:\n' +
  '- Terminal emulator (xterm.js) in playback mode\n' +
  '- Speed controls: 1x, 2x, 4x, 8x\n' +
  '- Timeline slider with event markers\n' +
  '- Session metadata sidebar: user, agent, protocol, duration, bytes, timestamps\n' +
  '- Export as asciinema cast button\n' +
  '- Keyboard controls: space=pause, left/right=seek, up/down=speed\n' +
  '\n' +
  '**6. Update internal/api/routes.go** -- register recording routes\n' +
  '**7. Update web/src/components/sidebar.tsx** -- add Shell Recordings nav (if admin)\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'session-recording', phase: 'SessionRec', model: 'sonnet' }
)

// Phase 6: Commit, push, close issues (haiku -- mechanical)
phase('Commit')
var commit = await agent(
  'Stage, commit, and push Sprint 1.5 implementation from ' + repoRoot + '.\n' +
  '\n' +
  'Run:\n' +
  '1. cd ' + repoRoot + '\n' +
  '2. git status\n' +
  '3. git add -A\n' +
  '4. git commit -m "Sprint 1.5: Script CRUD, 4-runtime executor, Monaco UI, remote shell, session recording" -m "- Script library CRUD with run history and output streaming" -m "- 4-runtime script executor: Bash, PowerShell, Python, Node with sandbox" -m "- Script UI with code editor, run detail with live output" -m "- SSH/WinRM remote shell with NATS-backed WebSocket proxy" -m "- Session recording with playback, export, and audit trail" -m "Closes #29, #30, #32, #33, #34"\n' +
  '5. git push origin main\n' +
  '6. for i in 29 30 32 33 34; do gh issue close $i -r completed; done\n' +
  '\n' +
  'If commit signing fails, retry with: git -c commit.gpgsign=false commit ...\n' +
  'Report the commit SHA and confirmation.',
  { label: 'commit-push', phase: 'Commit', model: 'haiku' }
)

return {
  status: 'Sprint 1.5 complete',
  phases: { scriptCRUD: scriptCRUD, scriptExec: scriptExec, scriptUI: scriptUI, remoteShell: remoteShell, sessionRec: sessionRec, commit: commit },
}
