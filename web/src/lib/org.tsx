import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  useMemo,
  type ReactNode,
} from 'react';
import { apiFetch } from './api';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type Role = 'admin' | 'operator' | 'technician' | 'viewer';

export type Permission =
  | 'settings:read'
  | 'settings:write'
  | 'a2a:read'
  | 'a2a:write'
  | 'shell:read'
  | 'shell:read:own'
  | 'shell:write'
  | 'agents:read'
  | 'agents:write'
  | 'checks:read'
  | 'checks:write'
  | 'alerts:read'
  | 'alerts:write'
  | 'policies:read'
  | 'policies:write'
  | 'patches:read'
  | 'patches:write'
  | 'patches:approve'
  | 'scripts:read'
  | 'scripts:write'
  | 'scripts:execute'
  | 'users:read'
  | 'users:write'
  | 'roles:read'
  | 'roles:write'
  | 'api-keys:read'
  | 'api-keys:write'
  | 'sso:read'
  | 'sso:write'
  | 'audit-log:read'
  | 'dashboard:read';

export interface Org {
  id: string;
  name: string;
}

export interface OrgContextValue {
  /** Current organization ID */
  orgId: string | null;
  /** Current site ID (may be null for org-level users) */
  siteId: string | null;
  /** User's role within the current org */
  role: Role | null;
  /** Human-readable org name */
  orgName: string | null;
  /** Full org list for multi-org users */
  orgs: Org[];
  /** All permissions derived from the current role */
  permissions: Set<Permission>;
  /** True while the initial /auth/me fetch is in progress */
  isLoading: boolean;
  /** Error from the last /auth/me call, if any */
  error: string | null;
  /** Switch to a different org (updates active org_id) */
  switchOrg: (orgId: string) => void;
}

// ---------------------------------------------------------------------------
// Permission matrix
// ---------------------------------------------------------------------------

const ROLE_PERMISSIONS: Record<Role, Permission[]> = {
  admin: [
    'settings:read', 'settings:write',
    'a2a:read', 'a2a:write',
    'shell:read', 'shell:read:own', 'shell:write',
    'agents:read', 'agents:write',
    'checks:read', 'checks:write',
    'alerts:read', 'alerts:write',
    'policies:read', 'policies:write',
    'patches:read', 'patches:write', 'patches:approve',
    'scripts:read', 'scripts:write', 'scripts:execute',
    'users:read', 'users:write',
    'roles:read', 'roles:write',
    'api-keys:read', 'api-keys:write',
    'sso:read', 'sso:write',
    'audit-log:read',
    'dashboard:read',
  ],
  operator: [
    'settings:read',
    'a2a:read', 'a2a:write',
    'shell:read',
    'agents:read', 'agents:write',
    'checks:read', 'checks:write',
    'alerts:read', 'alerts:write',
    'policies:read', 'policies:write',
    'patches:read', 'patches:write', 'patches:approve',
    'scripts:read', 'scripts:write', 'scripts:execute',
    'users:read',
    'api-keys:read',
    'audit-log:read',
    'dashboard:read',
  ],
  technician: [
    'shell:read:own',
    'agents:read',
    'checks:read', 'checks:write',
    'alerts:read', 'alerts:write',
    'patches:read',
    'scripts:read', 'scripts:execute',
    'dashboard:read',
  ],
  viewer: [
    'agents:read',
    'checks:read',
    'alerts:read',
    'policies:read',
    'patches:read',
    'scripts:read',
    'dashboard:read',
  ],
};

const VALID_ROLES: Role[] = ['admin', 'operator', 'technician', 'viewer'];

function normalizeRole(raw: string | undefined | null): Role | null {
  if (!raw) return null;
  const lower = raw.toLowerCase().trim();
  if (VALID_ROLES.includes(lower as Role)) return lower as Role;
  return null;
}

function buildPermissionSet(role: Role | null): Set<Permission> {
  if (!role) return new Set();
  return new Set(ROLE_PERMISSIONS[role] ?? []);
}

// ---------------------------------------------------------------------------
// API response shape for /auth/me
// ---------------------------------------------------------------------------

interface MeResponse {
  id: string;
  email: string;
  name?: string;
  role?: string;
  org_id?: string;
  org_name?: string;
  site_id?: string;
  orgs?: Org[];
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

const OrgContext = createContext<OrgContextValue | null>(null);

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

export interface OrgProviderProps {
  children: ReactNode;
}

export function OrgProvider({ children }: OrgProviderProps) {
  const [orgId, setOrgId] = useState<string | null>(null);
  const [siteId, setSiteId] = useState<string | null>(null);
  const [role, setRole] = useState<Role | null>(null);
  const [orgName, setOrgName] = useState<string | null>(null);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setIsLoading(true);
      setError(null);
      try {
        const res = await apiFetch<MeResponse>('/auth/me');
        if (cancelled) return;

        setOrgId(res.org_id ?? null);
        setSiteId(res.site_id ?? null);
        setRole(normalizeRole(res.role));
        setOrgName(res.org_name ?? null);
        setOrgs(res.orgs ?? []);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Failed to load user context');
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  const switchOrg = useCallback(
    (newOrgId: string) => {
      if (newOrgId === orgId) return;
      setOrgId(newOrgId);
      // Update the human-readable name from the cached org list
      const found = orgs.find((o) => o.id === newOrgId);
      if (found) setOrgName(found.name);
    },
    [orgId, orgs]
  );

  const permissions = useMemo(() => buildPermissionSet(role), [role]);

  const value = useMemo<OrgContextValue>(
    () => ({
      orgId,
      siteId,
      role,
      orgName,
      orgs,
      permissions,
      isLoading,
      error,
      switchOrg,
    }),
    [orgId, siteId, role, orgName, orgs, permissions, isLoading, error, switchOrg]
  );

  return <OrgContext.Provider value={value}>{children}</OrgContext.Provider>;
}

// ---------------------------------------------------------------------------
// useOrg — access the full org context
// ---------------------------------------------------------------------------

export function useOrg(): OrgContextValue {
  const ctx = useContext(OrgContext);
  if (!ctx) {
    throw new Error('useOrg must be used within an OrgProvider');
  }
  return ctx;
}

// ---------------------------------------------------------------------------
// usePermission — check a single permission
// ---------------------------------------------------------------------------

export function usePermission(action: Permission): boolean {
  const { permissions } = useOrg();
  return permissions.has(action);
}
