// useSettings — audit event types and query-string builder.
// Re-exported from useSettings.ts.

import type {
  User, InviteUserInput, UpdateUserInput,
  Role, CreateRoleInput, UpdateRoleInput,
  APIKey, CreateAPIKeyInput, CreateAPIKeyResult,
  SSOSSOProvider, CreateSSOProviderInput,
  UpdateSSOProviderInput, SSOTestResult,
} from './useSettings_types';

export type AuditOutcome = 'success' | 'failure' | 'denied';

// Shape returned by the useSettings hook.
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

// Build the query string for an audit-events fetch.
export function buildAuditQuery(filter?: AuditFilter): string {
  const params = new URLSearchParams();
  if (filter?.actor) params.set('actor', filter.actor);
  if (filter?.action) params.set('action', filter.action);
  if (filter?.resource_type) params.set('resource_type', filter.resource_type);
  if (filter?.from) params.set('from', filter.from);
  if (filter?.to) params.set('to', filter.to);
  if (filter?.outcome) params.set('outcome', filter.outcome);
  params.set('limit', String(filter?.limit ?? 200));
  if (filter?.offset) params.set('offset', String(filter.offset));
  return params.toString();
}
