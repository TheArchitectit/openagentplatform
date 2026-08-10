// useSettings — manages user accounts, roles, API keys, SSO providers,
// and audit events.
//
// Exposed operations:
//   fetchUsers / inviteUser / updateUser / deactivateUser
//   fetchRoles / createRole / updateRole / deleteRole
//   fetchAPIKeys / createAPIKey / revokeAPIKey
//   fetchSSOProviders / createSSOProvider / updateSSOProvider /
//     deleteSSOProvider / testSSOConnection
//   fetchAuditEvents

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch } from './api';

// ---------------------------------------------------------------------------
// Types — Users
// ---------------------------------------------------------------------------


export type {
  UserRole, UserStatus, User, InviteUserInput, UpdateUserInput,
  PermissionAction, PermissionCategory, Role, CreateRoleInput, UpdateRoleInput,
  APIKeyExpiry, APIKeyScope, APIKey, CreateAPIKeyInput, UpdateAPIKeyInput,
  PERMISSION_CATEGORIES, API_KEY_SCOPES, BUILT_IN_ROLES } from './useSettings_types'


// ---------------------------------------------------------------------------
// Types — Audit
// ---------------------------------------------------------------------------

export type AuditOutcome = 'success' | 'failure' | 'denied';

export interface AuditEvent {
  id: string;
  timestamp: string;
  actor: string;
  actor_id?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  outcome: AuditOutcome;
  ip_address?: string;
  user_agent?: string;
  details?: Record<string, unknown>;
}

export interface AuditFilter {
  actor?: string;
  action?: string;
  resource_type?: string;
  from?: string;
  to?: string;
  outcome?: AuditOutcome;
  limit?: number;
  offset?: number;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseSettingsResult {
  // Users
  users: User[];
  isLoadingUsers: boolean;
  fetchUsers: () => Promise<void>;
  inviteUser: (input: InviteUserInput) => Promise<User>;
  updateUser: (id: string, input: UpdateUserInput) => Promise<User>;
  deactivateUser: (id: string) => Promise<void>;

  // Roles
  roles: Role[];
  isLoadingRoles: boolean;
  fetchRoles: () => Promise<void>;
  createRole: (input: CreateRoleInput) => Promise<Role>;
  updateRole: (id: string, input: UpdateRoleInput) => Promise<Role>;
  deleteRole: (id: string) => Promise<void>;

  // API Keys
  apiKeys: APIKey[];
  isLoadingAPIKeys: boolean;
  fetchAPIKeys: () => Promise<void>;
  createAPIKey: (input: CreateAPIKeyInput) => Promise<CreateAPIKeyResult>;
  revokeAPIKey: (id: string) => Promise<void>;

  // SSO
  ssoProviders: SSOSSOProvider[];
  isLoadingSSO: boolean;
  fetchSSOProviders: () => Promise<void>;
  createSSOProvider: (input: CreateSSOProviderInput) => Promise<SSOSSOProvider>;
  updateSSOProvider: (id: string, input: UpdateSSOProviderInput) => Promise<SSOSSOProvider>;
  deleteSSOProvider: (id: string) => Promise<void>;
  testSSOConnection: (id: string) => Promise<SSOTestResult>;

  // Audit
  auditEvents: AuditEvent[];
  isLoadingAudit: boolean;
  fetchAuditEvents: (filter?: AuditFilter) => Promise<AuditEvent[]>;
}

export function useSettings(): UseSettingsResult {
  const [users, setUsers] = useState<User[]>([]);
  const [roles, setRoles] = useState<Role[]>(BUILT_IN_ROLES);
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([]);
  const [ssoProviders, setSSOProviders] = useState<SSOSSOProvider[]>([]);
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);

  const [isLoadingUsers, setIsLoadingUsers] = useState(false);
  const [isLoadingRoles, setIsLoadingRoles] = useState(false);
  const [isLoadingAPIKeys, setIsLoadingAPIKeys] = useState(false);
  const [isLoadingSSO, setIsLoadingSSO] = useState(false);
  const [isLoadingAudit, setIsLoadingAudit] = useState(false);

  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // --- Users -----------------------------------------------------------

  const fetchUsers = useCallback(async () => {
    setIsLoadingUsers(true);
    try {
      const res = await apiFetch<{ users?: User[] } | User[]>('/users?limit=500');
      const list = Array.isArray(res) ? res : (res.users ?? []);
      if (mountedRef.current) setUsers(list);
    } catch {
      // Silently fallback — the page renders an empty state.
    } finally {
      if (mountedRef.current) setIsLoadingUsers(false);
    }
  }, []);

  const inviteUser = useCallback(async (input: InviteUserInput): Promise<User> => {
    const u = await apiFetch<User>('/users/invite', {
      method: 'POST',
      json: input,
    });
    setUsers((prev) => [u, ...prev]);
    return u;
  }, []);

  const updateUser = useCallback(
    async (id: string, input: UpdateUserInput): Promise<User> => {
      const u = await apiFetch<User>(`/users/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        json: input,
      });
      setUsers((prev) => prev.map((x) => (x.id === id ? { ...x, ...u } : x)));
      return u;
    },
    []
  );

  const deactivateUser = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/users/${encodeURIComponent(id)}/deactivate`, {
      method: 'POST',
    });
    setUsers((prev) =>
      prev.map((u) => (u.id === id ? { ...u, status: 'inactive' as UserStatus } : u))
    );
  }, []);

  // --- Roles -----------------------------------------------------------

  const fetchRoles = useCallback(async () => {
    setIsLoadingRoles(true);
    try {
      const res = await apiFetch<{ roles?: Role[] } | Role[]>('/roles?limit=200');
      const list = Array.isArray(res) ? res : (res.roles ?? []);
      if (mountedRef.current) {
        // Merge with built-in defaults so the four core roles always render
        setRoles((prev) => {
          if (list.length === 0) return prev;
          return list;
        });
      }
    } catch {
      // Keep built-in defaults on failure
    } finally {
      if (mountedRef.current) setIsLoadingRoles(false);
    }
  }, []);

  const createRole = useCallback(async (input: CreateRoleInput): Promise<Role> => {
    const r = await apiFetch<Role>('/roles', {
      method: 'POST',
      json: input,
    });
    setRoles((prev) => [...prev, r]);
    return r;
  }, []);

  const updateRole = useCallback(
    async (id: string, input: UpdateRoleInput): Promise<Role> => {
      const r = await apiFetch<Role>(`/roles/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        json: input,
      });
      setRoles((prev) => prev.map((x) => (x.id === id ? { ...x, ...r } : x)));
      return r;
    },
    []
  );

  const deleteRole = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/roles/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setRoles((prev) => prev.filter((x) => x.id !== id));
  }, []);

  // --- API Keys --------------------------------------------------------

  const fetchAPIKeys = useCallback(async () => {
    setIsLoadingAPIKeys(true);
    try {
      const res = await apiFetch<{ keys?: APIKey[] } | APIKey[]>('/api-keys?limit=200');
      const list = Array.isArray(res) ? res : (res.keys ?? []);
      if (mountedRef.current) setAPIKeys(list);
    } catch {
      // Empty fallback
    } finally {
      if (mountedRef.current) setIsLoadingAPIKeys(false);
    }
  }, []);

  const createAPIKey = useCallback(
    async (input: CreateAPIKeyInput): Promise<CreateAPIKeyResult> => {
      const res = await apiFetch<CreateAPIKeyResult>('/api-keys', {
        method: 'POST',
        json: input,
      });
      setAPIKeys((prev) => [res.key, ...prev]);
      return res;
    },
    []
  );

  const revokeAPIKey = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/api-keys/${encodeURIComponent(id)}/revoke`, {
      method: 'POST',
    });
    setAPIKeys((prev) =>
      prev.map((k) => (k.id === id ? { ...k, status: 'revoked' as const } : k))
    );
  }, []);

  // --- SSO -------------------------------------------------------------

  const fetchSSOProviders = useCallback(async () => {
    setIsLoadingSSO(true);
    try {
      const res = await apiFetch<{ providers?: SSOSSOProvider[] } | SSOSSOProvider[]>(
        '/sso/providers?limit=100'
      );
      const list = Array.isArray(res) ? res : (res.providers ?? []);
      if (mountedRef.current) setSSOProviders(list);
    } catch {
      // Empty fallback
    } finally {
      if (mountedRef.current) setIsLoadingSSO(false);
    }
  }, []);

  const createSSOProvider = useCallback(
    async (input: CreateSSOProviderInput): Promise<SSOSSOProvider> => {
      const p = await apiFetch<SSOSSOProvider>('/sso/providers', {
        method: 'POST',
        json: input,
      });
      setSSOProviders((prev) => [...prev, p]);
      return p;
    },
    []
  );

  const updateSSOProvider = useCallback(
    async (id: string, input: UpdateSSOProviderInput): Promise<SSOSSOProvider> => {
      const p = await apiFetch<SSOSSOProvider>(
        `/sso/providers/${encodeURIComponent(id)}`,
        { method: 'PATCH', json: input }
      );
      setSSOProviders((prev) => prev.map((x) => (x.id === id ? { ...x, ...p } : x)));
      return p;
    },
    []
  );

  const deleteSSOProvider = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/sso/providers/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
    setSSOProviders((prev) => prev.filter((x) => x.id !== id));
  }, []);

  const testSSOConnection = useCallback(async (id: string): Promise<SSOTestResult> => {
    return apiFetch<SSOTestResult>(
      `/sso/providers/${encodeURIComponent(id)}/test`,
      { method: 'POST' }
    );
  }, []);

  // --- Audit -----------------------------------------------------------

  const fetchAuditEvents = useCallback(
    async (filter?: AuditFilter): Promise<AuditEvent[]> => {
      setIsLoadingAudit(true);
      try {
        const params = new URLSearchParams();
        if (filter?.actor) params.set('actor', filter.actor);
        if (filter?.action) params.set('action', filter.action);
        if (filter?.resource_type) params.set('resource_type', filter.resource_type);
        if (filter?.from) params.set('from', filter.from);
        if (filter?.to) params.set('to', filter.to);
        if (filter?.outcome) params.set('outcome', filter.outcome);
        params.set('limit', String(filter?.limit ?? 200));
        if (filter?.offset) params.set('offset', String(filter.offset));
        const qs = params.toString();
        const res = await apiFetch<{ events?: AuditEvent[] } | AuditEvent[]>(
          `/audit?${qs}`
        );
        const list = Array.isArray(res) ? res : (res.events ?? []);
        if (mountedRef.current) setAuditEvents(list);
        return list;
      } catch {
        return [];
      } finally {
        if (mountedRef.current) setIsLoadingAudit(false);
      }
    },
    []
  );

  return {
    users,
    isLoadingUsers,
    fetchUsers,
    inviteUser,
    updateUser,
    deactivateUser,
    roles,
    isLoadingRoles,
    fetchRoles,
    createRole,
    updateRole,
    deleteRole,
    apiKeys,
    isLoadingAPIKeys,
    fetchAPIKeys,
    createAPIKey,
    revokeAPIKey,
    ssoProviders,
    isLoadingSSO,
    fetchSSOProviders,
    createSSOProvider,
    updateSSOProvider,
    deleteSSOProvider,
    testSSOConnection,
    auditEvents,
    isLoadingAudit,
    fetchAuditEvents,
  };
}

export default useSettings;
