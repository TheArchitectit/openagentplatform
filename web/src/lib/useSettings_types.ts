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


export type UserRole = 'admin' | 'operator' | 'engineer' | 'viewer';
export type UserStatus = 'active' | 'inactive' | 'pending';

export interface User {
  id: string;
  name: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  last_login?: string;
  created_at?: string;
  avatar_url?: string;
}

export interface InviteUserInput {
  email: string;
  name: string;
  role: UserRole;
}

export interface UpdateUserInput {
  name?: string;
  role?: UserRole;
  status?: UserStatus;
}

// ---------------------------------------------------------------------------
// Types — Roles (re-exported from the catalog helper)
// ---------------------------------------------------------------------------

export type {
  PermissionAction, PermissionCategory, Role,
  CreateRoleInput, UpdateRoleInput,
} from './useSettings_types.helpers'
export { PERMISSION_CATEGORIES, API_KEY_SCOPES, BUILT_IN_ROLES } from './useSettings_types.helpers'

// ---------------------------------------------------------------------------
// Types — API Keys
// ---------------------------------------------------------------------------

export type APIKeyExpiry = '30d' | '90d' | '1yr' | 'custom' | 'never';

export interface APIKeyScope {
  key: string;
  label: string;
  category: string;
}

export interface APIKey {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  status: 'active' | 'revoked' | 'expired';
  created_by?: string;
}

export interface CreateAPIKeyInput {
  name: string;
  scopes: string[];
  expiry: APIKeyExpiry;
  custom_expiry?: string;
}

export interface CreateAPIKeyResult {
  key: APIKey;
  secret: string;
}

export interface UpdateAPIKeyInput {
  name?: string;
  scopes?: string[];
}

// ---------------------------------------------------------------------------
// Types — SSO
// ---------------------------------------------------------------------------

export type SSOProviderType = 'oidc' | 'saml';

export interface SSOSSOProvider {
  id: string;
  name: string;
  type: SSOProviderType;
  issuer_url: string;
  client_id: string;
  domain_whitelist: string[];
  attribute_mapping: Record<string, string>;
  status: 'active' | 'inactive' | 'error';
  is_default: boolean;
  created_at?: string;
}

export interface CreateSSOProviderInput {
  name: string;
  type: SSOProviderType;
  issuer_url: string;
  client_id: string;
  client_secret?: string;
  domain_whitelist: string[];
  attribute_mapping: Record<string, string>;
}

export interface UpdateSSOProviderInput {
  name?: string;
  issuer_url?: string;
  client_id?: string;
  client_secret?: string;
  domain_whitelist?: string[];
  attribute_mapping?: Record<string, string>;
  is_default?: boolean;
  status?: 'active' | 'inactive';
}

export interface SSOTestResult {
  success: boolean;
  message: string;
  latency_ms?: number;
  user_info_endpoint?: string;
}
