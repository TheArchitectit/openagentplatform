import { afterEach, describe, expect, it, vi } from 'vitest';
import { WebSocketConnection } from './connection';

class MockWebSocket {
  static readonly OPEN = 1;

  readyState = 0;
  sent: string[] = [];
  close = vi.fn(() => {
    this.readyState = 3;
    this.onclose?.(new CloseEvent('close'));
  });
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;

  send(data: string): void {
    this.sent.push(data);
  }

  open(): void {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event('open'));
  }
}

afterEach(() => {
  vi.useRealTimers();
});

describe('WebSocketConnection', () => {
  it('connects and forwards non-heartbeat messages', () => {
    const socket = new MockWebSocket();
    const onMessage = vi.fn();
    const connection = new WebSocketConnection({
      url: 'ws://test',
      webSocketFactory: () => socket as unknown as WebSocket,
      onMessage,
    });

    connection.connect();
    socket.open();
    socket.onmessage?.(new MessageEvent('message', { data: '{"type":"event"}' }));

    expect(connection.getStatus()).toBe('open');
    expect(onMessage).toHaveBeenCalledOnce();
    connection.close();
  });

  it('reconnects with exponential backoff', () => {
    vi.useFakeTimers();
    const sockets: MockWebSocket[] = [];
    const connection = new WebSocketConnection({
      url: 'ws://test',
      initialBackoffMs: 100,
      webSocketFactory: () => {
        const socket = new MockWebSocket();
        sockets.push(socket);
        return socket as unknown as WebSocket;
      },
    });

    connection.connect();
    sockets[0].onclose?.(new CloseEvent('close'));
    vi.advanceTimersByTime(99);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(2);
    connection.close();
  });

  it('sends ping frames and closes when pong is missing', () => {
    vi.useFakeTimers();
    const socket = new MockWebSocket();
    const connection = new WebSocketConnection({
      url: 'ws://test',
      heartbeatMs: 100,
      webSocketFactory: () => socket as unknown as WebSocket,
    });

    connection.connect();
    socket.open();
    vi.advanceTimersByTime(100);
    expect(socket.sent).toEqual(['{"type":"ping"}']);
    vi.advanceTimersByTime(100);
    expect(socket.close).toHaveBeenCalled();
    connection.close();
  });

  it('clears the heartbeat deadline when pong arrives', () => {
    vi.useFakeTimers();
    const socket = new MockWebSocket();
    const connection = new WebSocketConnection({
      url: 'ws://test',
      heartbeatMs: 100,
      webSocketFactory: () => socket as unknown as WebSocket,
    });

    connection.connect();
    socket.open();
    vi.advanceTimersByTime(100);
    socket.onmessage?.(new MessageEvent('message', { data: '{"type":"pong"}' }));
    vi.advanceTimersByTime(99);
    expect(socket.close).not.toHaveBeenCalled();
    connection.close();
  });
});
