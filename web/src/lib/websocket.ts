// WebSocket client with auto-reconnect, heartbeat, and channel subscriptions.
//
// The client maintains a single connection to the server and multiplexes
// many "channel" subscriptions on top. Each channel is a named stream of
// events; subscribers register a callback and receive parsed messages
// whose `channel` field matches.
//
// Connection lifecycle:
//   1. connect() is called from the constructor; on disconnect the client
//      schedules a reconnect with exponential backoff capped at 30s.
//   2. Every 30s a `{type: "ping"}` frame is sent. The server replies
//      with `{type: "pong"}`. If no pong arrives within the next 30s the
//      connection is considered dead and is closed (which triggers
//      reconnect).
//   3. To stop the client, call close().
//
// Auth: the cookie set by /auth/callback is automatically sent on the
// upgrade request by the browser. A `?token=` query parameter is also
// accepted as a fallback (e.g. for CLI clients).

export type Channel = 'agents' | 'checks' | 'alerts';

export interface WsEnvelope<T = unknown> {
  type: 'event' | 'ping' | 'pong' | 'subscribed' | 'unsubscribed' | 'error' | 'hello';
  channel?: Channel;
  event?: string;
  data?: T;
  message?: string;
}

export type Status =
  | 'connecting'
  | 'open'
  | 'closing'
  | 'closed';

export interface WsOptions {
  url?: string;
  heartbeatMs?: number;
  maxBackoffMs?: number;
  onStatusChange?: (s: Status) => void;
  onMessage?: (env: WsEnvelope) => void;
}

type ChannelHandler = (env: WsEnvelope) => void;

const DEFAULT_BASE_WS = 'ws://localhost:8080/ws';

function resolveUrl(): string {
  const env = (import.meta as ImportMeta).env;
  if (env?.VITE_WS_URL) return env.VITE_WS_URL;
  // If the API URL is http(s), derive ws(s) from it.
  if (env?.VITE_API_URL) {
    try {
      const u = new URL(env.VITE_API_URL);
      u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
      u.pathname = '/ws';
      return u.toString();
    } catch {
      /* fall through */
    }
  }
  return DEFAULT_BASE_WS;
}

export class WsClient {
  private url: string;
  private heartbeatMs: number;
  private maxBackoffMs: number;
  private onStatusChange?: (s: Status) => void;
  private onMessage?: (env: WsEnvelope) => void;

  private socket: WebSocket | null = null;
  private status: Status = 'closed';
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private pongDeadlineTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;

  private handlers = new Map<Channel, Set<ChannelHandler>>();
  private subscribed = new Set<Channel>();

  constructor(opts: WsOptions = {}) {
    this.url = opts.url ?? resolveUrl();
    this.heartbeatMs = opts.heartbeatMs ?? 30_000;
    this.maxBackoffMs = opts.maxBackoffMs ?? 30_000;
    this.onStatusChange = opts.onStatusChange;
    this.onMessage = opts.onMessage;
  }

  getStatus(): Status {
    return this.status;
  }

  connect(): void {
    if (this.status === 'open' || this.status === 'connecting') return;
    this.closedByUser = false;
    this.setStatus('connecting');
    try {
      this.socket = new WebSocket(this.url);
    } catch (err) {
      this.scheduleReconnect();
      return;
    }

    this.socket.onopen = () => {
      this.reconnectAttempt = 0;
      this.setStatus('open');
      // Re-subscribe to any channels the client was holding before.
      for (const ch of this.subscribed) {
        this.sendRaw({ type: 'subscribe', channel: ch });
      }
      this.startHeartbeat();
    };

    this.socket.onmessage = (ev) => {
      let env: WsEnvelope;
      try {
        env = JSON.parse(typeof ev.data === 'string' ? ev.data : '') as WsEnvelope;
      } catch {
        return;
      }
      this.onMessage?.(env);
      this.handleEnvelope(env);
    };

    this.socket.onerror = () => {
      // onclose will follow; let it handle reconnect.
    };

    this.socket.onclose = () => {
      this.stopHeartbeat();
      this.setStatus('closed');
      this.socket = null;
      if (!this.closedByUser) {
        this.scheduleReconnect();
      }
    };
  }

  close(): void {
    this.closedByUser = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.stopHeartbeat();
    if (this.socket) {
      this.setStatus('closing');
      try {
        this.socket.close();
      } catch {
        /* ignore */
      }
      this.socket = null;
    }
    this.setStatus('closed');
  }

  subscribe(channel: Channel, handler: ChannelHandler): () => void {
    let set = this.handlers.get(channel);
    if (!set) {
      set = new Set();
      this.handlers.set(channel, set);
    }
    set.add(handler);
    if (!this.subscribed.has(channel)) {
      this.subscribed.add(channel);
      if (this.status === 'open') {
        this.sendRaw({ type: 'subscribe', channel });
      }
    }
    return () => this.unsubscribe(channel, handler);
  }

  unsubscribe(channel: Channel, handler: ChannelHandler): void {
    const set = this.handlers.get(channel);
    if (!set) return;
    set.delete(handler);
    if (set.size === 0) {
      this.handlers.delete(channel);
      this.subscribed.delete(channel);
      if (this.status === 'open') {
        this.sendRaw({ type: 'unsubscribe', channel });
      }
    }
  }

  // --- internals -------------------------------------------------------

  private handleEnvelope(env: WsEnvelope): void {
    if (env.type === 'pong') {
      if (this.pongDeadlineTimer) {
        clearTimeout(this.pongDeadlineTimer);
        this.pongDeadlineTimer = null;
      }
      return;
    }
    if (env.type === 'event' && env.channel) {
      const set = this.handlers.get(env.channel);
      if (set) {
        for (const h of set) {
          try {
            h(env);
          } catch {
            /* swallow handler errors so one bad subscriber doesn't break others */
          }
        }
      }
    }
  }

  private sendRaw(payload: Record<string, unknown>): void {
    if (this.socket && this.socket.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(payload));
    }
  }

  private setStatus(s: Status): void {
    if (this.status === s) return;
    this.status = s;
    this.onStatusChange?.(s);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      this.sendRaw({ type: 'ping' });
      // If no pong within the next interval, drop the connection.
      if (this.pongDeadlineTimer) clearTimeout(this.pongDeadlineTimer);
      this.pongDeadlineTimer = setTimeout(() => {
        if (this.socket) {
          try {
            this.socket.close();
          } catch {
            /* ignore */
          }
        }
      }, this.heartbeatMs);
    }, this.heartbeatMs);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    if (this.pongDeadlineTimer) {
      clearTimeout(this.pongDeadlineTimer);
      this.pongDeadlineTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this.closedByUser) return;
    if (this.reconnectTimer) return;
    const attempt = ++this.reconnectAttempt;
    // Exponential backoff: 500ms, 1s, 2s, 4s, 8s, 16s, 30s, 30s, ...
    const base = Math.min(this.maxBackoffMs, 500 * 2 ** Math.min(attempt - 1, 6));
    const jitter = Math.floor(Math.random() * 250);
    const delay = base + jitter;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }
}

// --- Singleton ---------------------------------------------------------
//
// A single WebSocket per browser tab is usually what we want: every
// component that calls useAgents() ends up subscribing through this
// instance, and the server multiplexes channels onto the same socket.

let _instance: WsClient | null = null;

export function getWsClient(): WsClient {
  if (!_instance) {
    _instance = new WsClient();
    _instance.connect();
  }
  return _instance;
}

// Test/teardown helper.
export function _resetWsClient(): void {
  if (_instance) {
    _instance.close();
    _instance = null;
  }
}
