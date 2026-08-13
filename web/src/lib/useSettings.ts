// useSettings — user/role/API-key/SSO/audit management hook.

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch } from './api';

import { BUILT_IN_ROLES } from './useSettings_types'

// Local type bindings used by the hook body below.
import type {
  User, UserStatus, InviteUserInput, UpdateUserInput,
  Role, CreateRoleInput, UpdateRoleInput,
  APIKey, CreateAPIKeyInput, CreateAPIKeyResult,
  SSOSSOProvider, CreateSSOProviderInput,
  UpdateSSOProviderInput, SSOTestResult,
} from './useSettings_types'

// Public types/constants remain reachable from this path.
export type {
  UserRole, UserStatus, User, InviteUserInput, UpdateUserInput,
  PermissionAction, PermissionCategory, Role, CreateRoleInput, UpdateRoleInput,
  APIKeyExpiry, APIKeyScope, APIKey, CreateAPIKeyInput, UpdateAPIKeyInput,
  CreateAPIKeyResult, SSOProviderType, SSOSSOProvider, CreateSSOProviderInput,
  UpdateSSOProviderInput, SSOTestResult,
} from './useSettings_types'
export {
  PERMISSION_CATEGORIES, API_KEY_SCOPES, BUILT_IN_ROLES,
} from './useSettings_types'

// Audit event types + query builder (defined in useSettings.helpers.ts).
export type { AuditEvent, AuditFilter, UseSettingsResult, AuditOutcome } from './useSettings.helpers'
export { buildAuditQuery } from './useSettings.helpers'
import { buildAuditQuery } from './useSettings.helpers'
import type { AuditEvent, AuditFilter, UseSettingsResult } from './useSettings.helpers'

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
        const qs = buildAuditQuery(filter);
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
