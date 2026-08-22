import { describe, it, expect, vi, afterEach } from 'vitest';
import { fetchTasks } from './useA2A';
import type { A2ATask } from './useA2A';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('fetchTasks envelope contract', () => {
  it('parses the {tasks:[...], total:N} server envelope (P2-10)', async () => {
    const tasks: A2ATask[] = [
      { id: 't1', adapter: 'langgraph', status: 'completed', messages: [], artifacts: [], created_at: '2026-08-21T00:00:00Z' },
    ];
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response(JSON.stringify({ tasks, total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ));

    expect(await fetchTasks()).toEqual(tasks);
  });

  it('accepts the bare-array response the frontend dual-parses', async () => {
    const tasks: A2ATask[] = [
      { id: 't2', adapter: 'openai', status: 'working', messages: [], artifacts: [], created_at: '2026-08-21T00:00:01Z' },
    ];
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response(JSON.stringify(tasks), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ));

    expect(await fetchTasks()).toEqual(tasks);
  });
});
