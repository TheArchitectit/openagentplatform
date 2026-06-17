// Remote shell terminal — opens an interactive xterm.js terminal
// connected to the platform's WebSocket bridge for the given agent.
//
// Lifecycle:
//   1. On mount, POST /agents/{id}/shell to create a session and get
//      a ws_url. The server enforces RBAC and per-user/per-agent
//      session limits.
//   2. Open a WebSocket to ws_url. Wire xterm.js to the socket:
//      keystrokes -> {type:"stdin", data:base64} frames, and
//      {type:"stdout", data:base64} frames -> terminal.write().
//   3. Send a {type:"resize", cols, rows} frame whenever xterm's
//      fit addon reports a new size.
//   4. On unmount or explicit close, send a kill request and tear
//      down the socket.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ArrowLeft,
  Terminal as TerminalIcon,
  X,
  Maximize2,
  Minimize2,
  RefreshCw,
  AlertCircle,
} from 'lucide-react';
import { apiFetch, ApiError } from '@/lib/api';
import { getShellWsUrl } from '@/lib/websocket';

export const Route = createFileRoute('/agents/$agentId/shell')({
  component: RemoteShellPage,
});

interface CreateShellResponse {
  session_id: string;
  agent_id: string;
  protocol: string;
  ws_url: string;
  started_at: string;
}

interface TerminalWindow {
  Terminal: typeof import('@xterm/xterm').Terminal;
  FitAddon: { new (): import('@xterm/addon-fit').FitAddon };
}

declare global {
  interface Window {
    __oapXterm?: TerminalWindow;
  }
}

const XTERM_CSS = 'https://cdn.jsdelivr.net/npm/@xterm/xterm@5.5.0/css/xterm.css';
const XTERM_JS = 'https://cdn.jsdelivr.net/npm/@xterm/xterm@5.5.0/lib/xterm.js';
const FITADDON_JS = 'https://cdn.jsdelivr.net/npm/@xterm/addon-fit@0.10.0/lib/addon-fit.js';

type Status = 'idle' | 'creating' | 'connecting' | 'open' | 'closed' | 'error';

function RemoteShellPage() {
  const { agentId } = Route.useParams();
  const [status, setStatus] = useState<Status>('idle');
  const [error, setError] = useState<string | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [isFsActive, setIsFsActive] = useState(false);

  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<import('@xterm/xterm').Terminal | null>(null);
  const fitRef = useRef<import('@xterm/addon-fit').FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const resizeObsRef = useRef<ResizeObserver | null>(null);

  // Load xterm.js + addon-fit from CDN once.
  const xtermReadyRef = useRef<Promise<TerminalWindow> | null>(null);
  const loadXterm = useCallback(() => {
    if (xtermReadyRef.current) return xtermReadyRef.current;
    xtermReadyRef.current = (async () => {
      // Inject CSS.
      if (!document.querySelector('link[data-xterm]')) {
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = XTERM_CSS;
        link.dataset.xterm = 'true';
        document.head.appendChild(link);
      }
      await loadScript(XTERM_JS, 'xterm');
      await loadScript(FITADDON_JS, 'xterm-addon-fit');
      const w = window as unknown as { Terminal: typeof import('@xterm/xterm').Terminal; FitAddon: { FitAddon: { new (): import('@xterm/addon-fit').FitAddon } } };
      // FitAddon v0.10+ exports a class on window.FitAddon.FitAddon.
      const FitCtor = w.FitAddon?.FitAddon;
      if (!w.Terminal || !FitCtor) {
        throw new Error('xterm: failed to load from CDN');
      }
      return { Terminal: w.Terminal, FitAddon: FitCtor };
    })();
    return xtermReadyRef.current;
  }, []);

  // Create a shell session on the server.
  const createSession = useCallback(
    async (proto: 'ssh' | 'winrm') => {
      setStatus('creating');
      setError(null);
      try {
        const res = await apiFetch<CreateShellResponse>(
          `/agents/${encodeURIComponent(agentId)}/shell`,
          {
            method: 'POST',
            json: { protocol: proto, terminal_cols: 80, terminal_rows: 24 },
          },
        );
        setSessionId(res.session_id);
        return res;
      } catch (err) {
        const msg = err instanceof ApiError ? err.body || err.message : String(err);
        setError(msg);
        setStatus('error');
        return null;
      }
    },
    [agentId],
  );

  // Connect the WebSocket and wire xterm.
  const connect = useCallback(
    async (wsUrl: string, sid: string) => {
      try {
        const { Terminal, FitAddon } = await loadXterm();
        if (!containerRef.current) return;
        const term = new Terminal({
          cursorBlink: true,
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
          fontSize: 13,
          theme: {
            background: '#0b1120',
            foreground: '#e2e8f0',
            cursor: '#818cf8',
          },
        });
        const fit = new FitAddon();
        term.loadAddon(fit);
        term.open(containerRef.current);
        fit.fit();
        termRef.current = term;
        fitRef.current = fit;

        setStatus('connecting');
        const ws = new WebSocket(getShellWsUrl(wsUrl, sid));
        ws.binaryType = 'arraybuffer';
        wsRef.current = ws;

        ws.onopen = () => {
          setStatus('open');
          // Send an initial resize so the agent knows the size.
          sendResize();
          term.onData((d) => {
            if (ws.readyState === WebSocket.OPEN) {
              const b64 = btoa(unescape(encodeURIComponent(d)));
              ws.send(JSON.stringify({ type: 'stdin', data: b64 }));
            }
          });
        };
        ws.onmessage = (ev) => {
          try {
            const env = JSON.parse(typeof ev.data === 'string' ? ev.data : '');
            if (env.type === 'stdout' && env.data) {
              const bytes = atob(env.data);
              term.write(bytes);
            } else if (env.type === 'hello') {
              term.write('\x1b[2m--- connected ---\x1b[0m\r\n');
            }
          } catch {
            /* ignore */
          }
        };
        ws.onerror = () => {
          setError('WebSocket error');
          setStatus('error');
        };
        ws.onclose = (ev) => {
          term.write(`\r\n\x1b[2m--- disconnected (${ev.code}) ---\x1b[0m\r\n`);
          setStatus('closed');
        };

        // Watch the container and forward resizes.
        const obs = new ResizeObserver(() => sendResize());
        obs.observe(containerRef.current);
        resizeObsRef.current = obs;
        const onWinResize = () => sendResize();
        window.addEventListener('resize', onWinResize);
        // Cleanup hook (we use a wrapper so we can call it from closeSession):
        (ws as WebSocket & { __cleanup?: () => void }).__cleanup = () => {
          window.removeEventListener('resize', onWinResize);
          obs.disconnect();
        };
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setStatus('error');
      }
    },
    [loadXterm],
  );

  // Initial mount: create + connect.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const res = await createSession('ssh');
      if (cancelled || !res) return;
      await connect(res.ws_url, res.session_id);
    })();
    return () => {
      cancelled = true;
      cleanup();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Forward xterm resize -> WS resize.
  const sendResize = useCallback(() => {
    const term = termRef.current;
    const fit = fitRef.current;
    const ws = wsRef.current;
    if (!term || !fit || !ws || ws.readyState !== WebSocket.OPEN) return;
    try {
      fit.fit();
    } catch {
      /* fit can fail when container is hidden */
    }
    const { cols, rows } = term;
    ws.send(JSON.stringify({ type: 'resize', cols, rows }));
  }, []);

  // Close the session and tear down the socket.
  const cleanup = useCallback(() => {
    const ws = wsRef.current;
    if (ws) {
      const c = (ws as WebSocket & { __cleanup?: () => void }).__cleanup;
      if (c) c();
      try { ws.close(); } catch { /* ignore */ }
      wsRef.current = null;
    }
    if (resizeObsRef.current) {
      resizeObsRef.current.disconnect();
      resizeObsRef.current = null;
    }
    if (termRef.current) {
      termRef.current.dispose();
      termRef.current = null;
    }
    fitRef.current = null;
  }, []);

  const closeSession = useCallback(async () => {
    if (sessionId) {
      try {
        await apiFetch(`/shell/${encodeURIComponent(sessionId)}/kill`, {
          method: 'POST',
        });
      } catch {
        /* best-effort */
      }
    }
    cleanup();
    setStatus('closed');
  }, [sessionId, cleanup]);

  // Reconnect: keep the session, just open a new socket.
  const reconnect = useCallback(async () => {
    if (!sessionId) return;
    cleanup();
    setStatus('connecting');
    try {
      // Reuse the existing session by re-fetching status to confirm
      // it's still alive; if not, the server returns 404 and we
      // create a new one.
      await apiFetch(`/shell/${encodeURIComponent(sessionId)}`);
      // We need a fresh ws_url — easiest path is to create a new
      // session. Kill the old one first.
      await apiFetch(`/shell/${encodeURIComponent(sessionId)}/kill`, { method: 'POST' });
    } catch {
      /* fall through and create a new session */
    }
    const res = await createSession('ssh');
    if (res) await connect(res.ws_url, res.session_id);
  }, [sessionId, cleanup, createSession, connect]);

  // Fullscreen toggle.
  const toggleFs = useCallback(() => {
    const next = !isFullscreen;
    setIsFullscreen(next);
    setIsFsActive(next);
    // Allow layout to settle before re-fitting the terminal.
    requestAnimationFrame(() => sendResize());
  }, [isFullscreen, sendResize]);

  return (
    <div className={isFullscreen ? 'fixed inset-0 z-50 bg-surface-primary flex flex-col' : 'space-y-4'}>
      {/* Header / toolbar */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/agents/$agentId"
            params={{ agentId }}
            className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center hover:bg-border-strong"
          >
            <ArrowLeft className="h-4 w-4 text-text-secondary" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-accent/20 border border-accent/30 flex items-center justify-center">
            <TerminalIcon className="h-4 w-4 text-accent" />
          </div>
          <div>
            <h1 className="text-xl font-bold text-text-primary">Remote shell</h1>
            <p className="text-text-secondary text-xs font-mono">{agentId}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatusPill status={status} />
          <button
            type="button"
            onClick={reconnect}
            disabled={status === 'creating' || status === 'connecting'}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary hover:bg-border-strong disabled:opacity-50"
            title="Reconnect"
          >
            <RefreshCw className="h-4 w-4" />
            <span>Reconnect</span>
          </button>
          <button
            type="button"
            onClick={toggleFs}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary hover:bg-border-strong"
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
          <button
            type="button"
            onClick={closeSession}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-danger/20 border border-danger/40 text-sm text-danger hover:bg-danger/30"
            title="Close session"
          >
            <X className="h-4 w-4" />
            <span>Close</span>
          </button>
        </div>
      </div>

      {/* Disconnect banner */}
      {(status === 'closed' || status === 'error') && (
        <div className="rounded-md border border-warning/30 bg-warning/10 p-3 flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-warning text-sm">
            <AlertCircle className="h-4 w-4" />
            <span>
              {status === 'error' && error ? `Connection error: ${error}` : 'Session closed.'}
            </span>
          </div>
          <button
            type="button"
            onClick={reconnect}
            className="inline-flex items-center gap-1 px-3 h-8 rounded-md bg-warning/20 border border-warning/40 text-sm text-warning hover:bg-warning/30"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Reconnect
          </button>
        </div>
      )}

      {/* Terminal container */}
      <div
        className={
          isFullscreen
            ? 'flex-1 bg-surface-primary p-2'
            : 'rounded-lg border border-border-subtle bg-surface-primary p-2'
        }
        style={isFullscreen ? { minHeight: 0 } : { height: '60vh', minHeight: 360 }}
      >
        <div
          ref={containerRef}
          className="w-full h-full"
          // xterm writes into this div.
        />
      </div>

      {isFsActive && !isFullscreen && null}
    </div>
  );
}

function StatusPill({ status }: { status: Status }) {
  const map: Record<Status, { label: string; cls: string }> = {
    idle: { label: 'idle', cls: 'bg-border-strong/40 text-text-secondary border-border-strong' },
    creating: { label: 'creating', cls: 'bg-warning/20 text-warning border-warning/40' },
    connecting: { label: 'connecting', cls: 'bg-warning/20 text-warning border-warning/40' },
    open: { label: 'connected', cls: 'bg-success/20 text-success border-success/40' },
    closed: { label: 'closed', cls: 'bg-border-strong/40 text-text-secondary border-border-strong' },
    error: { label: 'error', cls: 'bg-danger/20 text-danger border-danger/40' },
  };
  const v = map[status];
  return (
    <span
      className={`inline-flex items-center px-2.5 h-7 rounded-full text-xs border ${v.cls}`}
    >
      {v.label}
    </span>
  );
}

function loadScript(src: string, globalName: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if ((window as Record<string, unknown>)[globalName]) {
      resolve();
      return;
    }
    const s = document.createElement('script');
    s.src = src;
    s.async = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error(`failed to load ${src}`));
    document.head.appendChild(s);
  });
}
