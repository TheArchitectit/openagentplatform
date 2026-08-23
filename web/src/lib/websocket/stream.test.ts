import { describe, expect, it, vi } from 'vitest';
import { EventStream } from './stream';

describe('EventStream', () => {
  it('subscribes once and dispatches only matching events', () => {
    const source = { send: vi.fn(() => true) };
    const stream = new EventStream(source);
    const first = vi.fn();
    const second = vi.fn();

    stream.subscribe('kpi.updated', first);
    stream.subscribe('kpi.updated', second);
    stream.subscribe('alert.created', vi.fn());
    stream.dispatch(JSON.stringify({
      type: 'kpi.updated',
      data: { key: 'activeAgents', value: 7 },
      timestamp: '2026-08-22T00:00:00Z',
    }));

    expect(source.send).toHaveBeenCalledWith('{"type":"subscribe","event":"kpi.updated"}');
    expect(first).toHaveBeenCalledOnce();
    expect(second).toHaveBeenCalledOnce();
  });

  it('unsubscribes remotely after the final handler is removed', () => {
    const source = { send: vi.fn(() => true) };
    const stream = new EventStream(source);
    const handler = vi.fn();
    const unsubscribe = stream.subscribe('task.updated', handler);

    unsubscribe();

    expect(source.send).toHaveBeenLastCalledWith('{"type":"unsubscribe","event":"task.updated"}');
  });

  it('ignores malformed events and isolates handler failures', () => {
    const source = { send: vi.fn(() => true) };
    const stream = new EventStream(source);
    const healthy = vi.fn();
    stream.subscribe('alert.created', () => { throw new Error('subscriber failed'); });
    stream.subscribe('alert.created', healthy);

    expect(stream.dispatch('not-json')).toBe(false);
    expect(stream.dispatch(JSON.stringify({
      type: 'alert.created',
      data: { id: 'a1', severity: 'warning', message: 'Check failed' },
      timestamp: '2026-08-22T00:00:00Z',
    }))).toBe(true);
    expect(healthy).toHaveBeenCalledOnce();
  });
});
