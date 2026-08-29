import { describe, it, expect, vi, afterEach } from 'vitest';
import { fetchApprovalDetail } from './useApprovals';
import type { ApprovalDetail, ApprovalRequest } from './useApprovals_types';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const baseApproval: ApprovalRequest = {
  id: 'ap-1',
  action_type: 'patch_deploy',
  payload: { task_id: 't1' },
  requester_agent_id: 'agent-7',
  urgency: 'high',
  status: 'pending',
  escalation_depth: 0,
  created_at: '2026-08-24T00:00:00Z',
  expires_at: '2026-08-24T04:00:00Z',
  notifications_sent: 0,
};

describe('approval REST contracts (spec R6)', () => {
  it('fetchApprovalDetail parses the flattened detail with history (R1.3/R6.2)', async () => {
    const detail: ApprovalDetail = { ...baseApproval, history: [] };
    const fetchMock = vi.fn(async () => jsonResponse(detail));
    vi.stubGlobal('fetch', fetchMock);

    const got = await fetchApprovalDetail('ap-1');
    expect(got.status).toBe('pending');
    expect(got.history).toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/a2a/approvals/ap-1',
      expect.objectContaining({ credentials: 'include' })
    );
  });

  it('URL-encodes the approval id path segment', async () => {
    const fetchMock = vi.fn(
      async (_url: string, _init?: RequestInit) => jsonResponse({ ...baseApproval, history: [] })
    );
    vi.stubGlobal('fetch', fetchMock);

    await fetchApprovalDetail('a/b?c');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/a2a/approvals/a%2Fb%3Fc');
  });

  it('surfaces a 404 as ApiError with status (queue deep link on deleted id)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ error: 'not_found' }, 404)));

    await expect(fetchApprovalDetail('missing')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
    });
  });

  it('decision endpoints take the id and a JSON body (used by useApprovals approve/reject)', async () => {
    const decided: ApprovalRequest = {
      ...baseApproval,
      status: 'approved',
      decided_by: 'ops@example.com',
    };
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => jsonResponse(decided));
    vi.stubGlobal('fetch', fetchMock);

    const { apiFetch } = await import('./api');
    const got = await apiFetch<ApprovalRequest>(
      '/a2a/approvals/ap-1/approve',
      { method: 'POST', json: { comment: 'ship it' } }
    );
    expect(got.status).toBe('approved');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/v1/a2a/approvals/ap-1/approve');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ comment: 'ship it' });
  });
});
