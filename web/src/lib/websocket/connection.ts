export type ConnectionStatus = 'connecting' | 'open' | 'closing' | 'closed';

export interface WebSocketConnectionOptions {
  url: string;
  heartbeatMs?: number;
  maxBackoffMs?: number;
  initialBackoffMs?: number;
  webSocketFactory?: (url: string) => WebSocket;
  onStatusChange?: (status: ConnectionStatus) => void;
  onMessage?: (event: MessageEvent) => void;
}

const DEFAULT_HEARTBEAT_MS = 30_000;
const DEFAULT_INITIAL_BACKOFF_MS = 500;
const DEFAULT_MAX_BACKOFF_MS = 30_000;

export class WebSocketConnection {
  private readonly url: string;
  private readonly heartbeatMs: number;
  private readonly maxBackoffMs: number;
  private readonly initialBackoffMs: number;
  private readonly webSocketFactory: (url: string) => WebSocket;
  private readonly onStatusChange?: (status: ConnectionStatus) => void;
  private readonly onMessage?: (event: MessageEvent) => void;

  private socket: WebSocket | null = null;
  private status: ConnectionStatus = 'closed';
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private closedByUser = false;

  constructor(options: WebSocketConnectionOptions) {
    this.url = options.url;
    this.heartbeatMs = options.heartbeatMs ?? DEFAULT_HEARTBEAT_MS;
    this.maxBackoffMs = options.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
    this.initialBackoffMs = options.initialBackoffMs ?? DEFAULT_INITIAL_BACKOFF_MS;
    this.webSocketFactory = options.webSocketFactory ?? ((url) => new WebSocket(url));
    this.onStatusChange = options.onStatusChange;
    this.onMessage = options.onMessage;
  }

  getStatus(): ConnectionStatus {
    return this.status;
  }

  connect(): void {
    if (this.status === 'connecting' || this.status === 'open') return;

    this.closedByUser = false;
    this.setStatus('connecting');

    try {
      this.socket = this.webSocketFactory(this.url);
    } catch {
      this.setStatus('closed');
      this.scheduleReconnect();
      return;
    }

    this.socket.onopen = () => {
      this.reconnectAttempt = 0;
      this.setStatus('open');
      this.startHeartbeat();
    };

    this.socket.onmessage = (event) => {
      if (this.isPong(event.data)) {
        this.clearPongTimer();
        return;
      }
      this.onMessage?.(event);
    };

    this.socket.onerror = () => {
      // The close event owns reconnect scheduling.
    };

    this.socket.onclose = () => {
      this.stopHeartbeat();
      this.socket = null;
      this.setStatus('closed');
      if (!this.closedByUser) this.scheduleReconnect();
    };
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): boolean {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return false;
    this.socket.send(data);
    return true;
  }

  close(): void {
    this.closedByUser = true;
    this.clearReconnectTimer();
    this.stopHeartbeat();

    if (this.socket) {
      this.setStatus('closing');
      this.socket.close();
      this.socket = null;
    }
    this.setStatus('closed');
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      if (this.pongTimer) return;
      if (!this.send(JSON.stringify({ type: 'ping' }))) return;
      this.pongTimer = setTimeout(() => this.socket?.close(), this.heartbeatMs);
    }, this.heartbeatMs);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    this.clearPongTimer();
  }

  private clearPongTimer(): void {
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this.closedByUser || this.reconnectTimer) return;

    const delay = Math.min(
      this.maxBackoffMs,
      this.initialBackoffMs * 2 ** this.reconnectAttempt,
    );
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private setStatus(status: ConnectionStatus): void {
    if (this.status === status) return;
    this.status = status;
    this.onStatusChange?.(status);
  }

  private isPong(data: unknown): boolean {
    if (typeof data !== 'string') return false;
    try {
      const value = JSON.parse(data) as { type?: unknown };
      return value.type === 'pong';
    } catch {
      return false;
    }
  }
}
