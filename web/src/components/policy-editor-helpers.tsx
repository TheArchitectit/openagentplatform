// PolicyEditor — modal form for creating or editing a policy.
//
// Allows the user to set metadata (name, description, category, severity,
// enforcement mode) and edit the Rego policy body using a Monaco editor
// (loaded from jsDelivr CDN at runtime, with a textarea fallback). The
// editor supports a "Validate" syntax check (via the policies validate
// endpoint) and a "Save" action (create or update). A template picker
// lets the user load a built-in Rego template.

import { useEffect, useId, useState } from 'react';
import {
  X,
  Save,
  CheckCircle2,
  AlertCircle,
  FileCode2,
  Loader2,
} from 'lucide-react';
import type {
  Policy,
  CreatePolicyInput,
  UpdatePolicyInput,
  PolicyValidationResult,
  PolicyCategory,
  PolicySeverity,
  PolicyEnforcement,
} from '@/lib/usePolicies';
import { MonacoEditor } from '@/components/monaco-editor';


export interface PolicyTemplate {
  id: string;
  label: string;
  description: string;
  category: PolicyCategory;
  severity: PolicySeverity;
  enforcement: PolicyEnforcement;
  rego_source: string;
}

const TEMPLATES: PolicyTemplate[] = [
  {
    id: 'ssh-root-disabled',
    label: 'SSH: Disable root login',
    description: 'Ensures PermitRootLogin is set to no in sshd_config.',
    category: 'security',
    severity: 'critical',
    enforcement: 'enforce',
    rego_source: `package policies.ssh

# Deny if root login is permitted over SSH.
deny[result] {
  input.sshd.permit_root_login == "yes"
  result := "PermitRootLogin must be 'no'"
}
`,
  },
  {
    id: 'tls-1.2-min',
    label: 'TLS: Minimum version 1.2',
    description: 'Reject endpoints advertising TLS < 1.2.',
    category: 'compliance',
    severity: 'warning',
    enforcement: 'audit',
    rego_source: `package policies.tls

deny[result] {
  input.tls.min_version == "1.0"
  result := "TLS 1.0 is end-of-life"
}

deny[result] {
  input.tls.min_version == "1.1"
  result := "TLS 1.1 is end-of-life"
}
`,
  },
  {
    id: 'disk-max-usage',
    label: 'Disk: max 80% usage',
    description: 'Alert when any mounted filesystem exceeds 80% utilization.',
    category: 'performance',
    severity: 'warning',
    enforcement: 'report',
    rego_source: `package policies.disk

deny[result] {
  input.filesystem.used_pct > 80
  result := sprintf("Filesystem %s is %d%% full", [input.filesystem.mount, input.filesystem.used_pct])
}
`,
  },
  {
    id: 'password-min-length',
    label: 'Password: min length 12',
    description: 'Enforce minimum password length of 12 characters.',
    category: 'security',
    severity: 'warning',
    enforcement: 'enforce',
    rego_source: `package policies.password

deny[result] {
  input.password.min_length < 12
  result := sprintf("min_length is %d, must be >= 12", [input.password.min_length])
}
`,
  },
];

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface PolicyEditorProps {
  policy?: Policy | null;
  onClose: () => void;
  onSave: (input: CreatePolicyInput | { id: string; input: UpdatePolicyInput }) => Promise<void>;
  validateRego: (regoSource: string) => Promise<PolicyValidationResult>;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

const CATEGORY_OPTIONS: { value: PolicyCategory; label: string }[] = [
  { value: 'security', label: 'Security' },
  { value: 'compliance', label: 'Compliance' },
  { value: 'configuration', label: 'Configuration' },
  { value: 'performance', label: 'Performance' },
  { value: 'custom', label: 'Custom' },
];

const SEVERITY_OPTIONS: { value: PolicySeverity; label: string }[] = [
  { value: 'info', label: 'Info' },
  { value: 'warning', label: 'Warning' },
  { value: 'critical', label: 'Critical' },
  { value: 'emergency', label: 'Emergency' },
];

const ENFORCEMENT_OPTIONS: { value: PolicyEnforcement; label: string }[] = [
  { value: 'enforce', label: 'Enforce' },
  { value: 'audit', label: 'Audit' },
  { value: 'report', label: 'Report' },
];

export function fieldClasses(): string {
  return 'w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40';
}

export function labelClasses(): string {
  return 'block text-xs font-medium text-gray-300 mb-1';
}

