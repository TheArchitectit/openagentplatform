import { describe, expect, it } from 'vitest';
import { isDashboardEvent, isDashboardEventType } from './events';

describe('dashboard events', () => {
  it('recognizes supported dashboard event types', () => {
    expect(isDashboardEventType('kpi.updated')).toBe(true);
    expect(isDashboardEventType('agent.status.changed')).toBe(true);
    expect(isDashboardEventType('unknown')).toBe(false);
  });

  it('validates event envelopes', () => {
    expect(isDashboardEvent({
      type: 'task.updated',
      data: { taskId: 'task-1', status: 'working' },
      timestamp: '2026-08-22T00:00:00Z',
    })).toBe(true);
    expect(isDashboardEvent({ type: 'task.updated', data: null })).toBe(false);
  });
});
