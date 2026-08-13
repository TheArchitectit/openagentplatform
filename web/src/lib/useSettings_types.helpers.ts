// useSettings — permission catalog + built-in role constants.
// Shared by useSettings_types.ts and the roles/settings UI.

export type PermissionAction = 'read' | 'write' | 'admin';

export interface PermissionCategory {
  key: string;
  label: string;
  actions: PermissionAction[];
}

export interface Role {
  id: string;
  name: string;
  description: string;
  built_in: boolean;
  user_count: number;
  permission_count: number;
  permissions: string[];
}

export interface CreateRoleInput {
  name: string;
  description: string;
  permissions: string[];
}

export interface UpdateRoleInput {
  name?: string;
  description?: string;
  permissions?: string[];
}

export const PERMISSION_CATEGORIES: PermissionCategory[] = [
  {
    key: 'agents',
    label: 'Agents',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'checks',
    label: 'Checks',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'alerts',
    label: 'Alerts',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'policies',
    label: 'Policies',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'patches',
    label: 'Patches',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'scripts',
    label: 'Scripts',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'remote',
    label: 'Remote Shell',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'a2a',
    label: 'A2A',
    actions: ['read', 'write', 'admin'],
  },
  {
    key: 'settings',
    label: 'Settings',
    actions: ['read', 'write', 'admin'],
  },
];

export const API_KEY_SCOPES: APIKeyScope[] = [
  { key: 'agents:read', label: 'Agents: Read', category: 'agents' },
  { key: 'agents:write', label: 'Agents: Write', category: 'agents' },
  { key: 'checks:read', label: 'Checks: Read', category: 'checks' },
  { key: 'checks:write', label: 'Checks: Write', category: 'checks' },
  { key: 'alerts:read', label: 'Alerts: Read', category: 'alerts' },
  { key: 'alerts:write', label: 'Alerts: Write', category: 'alerts' },
  { key: 'policies:read', label: 'Policies: Read', category: 'policies' },
  { key: 'policies:write', label: 'Policies: Write', category: 'policies' },
  { key: 'patches:read', label: 'Patches: Read', category: 'patches' },
  { key: 'patches:write', label: 'Patches: Write', category: 'patches' },
  { key: 'scripts:read', label: 'Scripts: Read', category: 'scripts' },
  { key: 'scripts:write', label: 'Scripts: Write', category: 'scripts' },
  { key: 'remote:read', label: 'Remote Shell: Read', category: 'remote' },
  { key: 'remote:write', label: 'Remote Shell: Write', category: 'remote' },
  { key: 'audit:read', label: 'Audit Log: Read', category: 'settings' },
  { key: 'settings:read', label: 'Settings: Read', category: 'settings' },
  { key: 'settings:write', label: 'Settings: Write', category: 'settings' },
  { key: 'a2a:send', label: 'A2A: Send', category: 'a2a' },
  { key: 'a2a:read', label: 'A2A: Read', category: 'a2a' },
];

export interface APIKeyScope {
  key: string;
  label: string;
  category: string;
}

// Built-in role definitions (used as defaults when the API is unavailable)
export const BUILT_IN_ROLES: Role[] = [
  {
    id: 'role-admin',
    name: 'admin',
    description: 'Full access to all resources and settings',
    built_in: true,
    user_count: 0,
    permission_count: PERMISSION_CATEGORIES.length * 3,
    permissions: PERMISSION_CATEGORIES.flatMap((c) =>
      c.actions.map((a) => `${c.key}:${a}`)
    ),
  },
  {
    id: 'role-operator',
    name: 'operator',
    description: 'Day-to-day operational access — read all, write most',
    built_in: true,
    user_count: 0,
    permission_count: 0,
    permissions: [
      'agents:read',
      'agents:write',
      'checks:read',
      'checks:write',
      'alerts:read',
      'alerts:write',
      'policies:read',
      'patches:read',
      'patches:write',
      'scripts:read',
      'scripts:write',
      'remote:read',
      'remote:write',
      'audit:read',
      'a2a:read',
      'a2a:send',
    ],
  },
  {
    id: 'role-engineer',
    name: 'engineer',
    description: 'Technical access for engineering tasks',
    built_in: true,
    user_count: 0,
    permission_count: 0,
    permissions: [
      'agents:read',
      'checks:read',
      'alerts:read',
      'alerts:write',
      'policies:read',
      'patches:read',
      'scripts:read',
      'scripts:write',
      'remote:read',
      'remote:write',
      'a2a:read',
      'a2a:send',
    ],
  },
  {
    id: 'role-viewer',
    name: 'viewer',
    description: 'Read-only access to dashboards and reports',
    built_in: true,
    user_count: 0,
    permission_count: 0,
    permissions: [
      'agents:read',
      'checks:read',
      'alerts:read',
      'policies:read',
      'patches:read',
      'scripts:read',
      'a2a:read',
    ],
  },
];
