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

// ---------------------------------------------------------------------------
// Built-in templates
// ---------------------------------------------------------------------------

interface PolicyTemplate {
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

function fieldClasses(): string {
  return 'w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40';
}

function labelClasses(): string {
  return 'block text-xs font-medium text-gray-300 mb-1';
}

export function PolicyEditor({ policy, onClose, onSave, validateRego }: PolicyEditorProps) {
  const isEdit = Boolean(policy?.id);

  // Form state
  const [name, setName] = useState(policy?.name ?? '');
  const [description, setDescription] = useState(policy?.description ?? '');
  const [category, setCategory] = useState<PolicyCategory>(policy?.category ?? 'security');
  const [severity, setSeverity] = useState<PolicySeverity>(policy?.severity ?? 'warning');
  const [enforcement, setEnforcement] = useState<PolicyEnforcement>(
    policy?.enforcement ?? 'enforce'
  );
  const [regoSource, setRegoSource] = useState(
    policy?.rego_source ??
      `package policies.custom

# TODO: define your policy. Use "deny" rules to reject input.
deny[result] {
  input.example.flag == true
  result := "example flag is set"
}
`
  );

  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);
  const [validation, setValidation] = useState<PolicyValidationResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [templateId, setTemplateId] = useState<string>('');

  // When the user picks a template, fill the form with its contents.
  const applyTemplate = (id: string) => {
    setTemplateId(id);
    if (!id) return;
    const t = TEMPLATES.find((x) => x.id === id);
    if (!t) return;
    setName((prev) => prev || t.label);
    setDescription((prev) => prev || t.description);
    setCategory(t.category);
    setSeverity(t.severity);
    setEnforcement(t.enforcement);
    setRegoSource(t.rego_source);
    setValidation(null);
  };

  // Validation
  const handleValidate = async () => {
    setValidating(true);
    setError(null);
    try {
      const result = await validateRego(regoSource);
      setValidation(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Validation failed');
    } finally {
      setValidating(false);
    }
  };

  // Save
  const handleSave = async () => {
    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (!regoSource.trim()) {
      setError('Rego source is required');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      if (isEdit && policy) {
        await onSave({
          id: policy.id,
          input: {
            name: name.trim(),
            description: description.trim() || undefined,
            category,
            severity,
            enforcement,
            rego_source: regoSource,
          },
        });
      } else {
        const input: CreatePolicyInput = {
          name: name.trim(),
          description: description.trim() || undefined,
          category,
          severity,
          enforcement,
          rego_source: regoSource,
          enabled: true,
        };
        await onSave(input);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  const baseId = useId();
  const titleId = `${baseId}-title`;
  const templateFieldId = `${baseId}-template`;
  const nameId = `${baseId}-name`;
  const categoryId = `${baseId}-category`;
  const severityId = `${baseId}-severity`;
  const enforcementId = `${baseId}-enforcement`;
  const descriptionId = `${baseId}-description`;
  const regoId = `${baseId}-rego`;
  const errorId = `${baseId}-error`;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 overflow-y-auto"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-3xl rounded-xl border border-slate-800 bg-slate-900 shadow-2xl my-8">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
          <div className="flex items-center gap-2">
            <FileCode2 className="h-4 w-4 text-blue-400" aria-hidden="true" />
            <h2 id={titleId} className="text-sm font-semibold text-white">
              {isEdit ? 'Edit Policy' : 'Create Policy'}
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1.5 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>

        {/* Body */}
        <div className="p-5 space-y-4 max-h-[70vh] overflow-y-auto">
          {/* Template picker */}
          {!isEdit && (
            <div>
              <label htmlFor={templateFieldId} className={labelClasses()}>Template</label>
              <select
                id={templateFieldId}
                value={templateId}
                onChange={(e) => applyTemplate(e.target.value)}
                className={fieldClasses()}
              >
                <option value="">— Choose a built-in template —</option>
                {TEMPLATES.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.label}
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-400 mt-1">
                Templates pre-fill the fields below. You can still edit everything.
              </p>
            </div>
          )}

          {/* Metadata grid */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label htmlFor={nameId} className={labelClasses()}>
                Name <span aria-hidden="true" className="text-red-400">*</span>
              </label>
              <input
                id={nameId}
                type="text"
                required
                aria-required="true"
                aria-invalid={error === 'Name is required' ? 'true' : undefined}
                aria-describedby={error === 'Name is required' ? errorId : undefined}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Disable SSH root login"
                className={fieldClasses()}
              />
            </div>
            <div>
              <label htmlFor={categoryId} className={labelClasses()}>Category</label>
              <select
                id={categoryId}
                value={category}
                onChange={(e) => setCategory(e.target.value as PolicyCategory)}
                className={fieldClasses()}
              >
                {CATEGORY_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor={severityId} className={labelClasses()}>Severity</label>
              <select
                id={severityId}
                value={severity}
                onChange={(e) => setSeverity(e.target.value as PolicySeverity)}
                className={fieldClasses()}
              >
                {SEVERITY_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor={enforcementId} className={labelClasses()}>Enforcement</label>
              <select
                id={enforcementId}
                value={enforcement}
                onChange={(e) => setEnforcement(e.target.value as PolicyEnforcement)}
                className={fieldClasses()}
              >
                {ENFORCEMENT_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label htmlFor={descriptionId} className={labelClasses()}>Description</label>
            <input
              id={descriptionId}
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What this policy enforces and why"
              className={fieldClasses()}
            />
          </div>

          {/* Rego editor */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label htmlFor={regoId} className={labelClasses() + ' mb-0'}>Rego source</label>
              <span className="text-xs text-gray-400" aria-hidden="true">package policies.&lt;name&gt;</span>
            </div>
            <div className="rounded-xl border border-slate-800 overflow-hidden">
              <MonacoEditor
                value={regoSource}
                onChange={(v) => {
                  setRegoSource(v);
                  setValidation(null);
                }}
                language="rego"
                height={320}
                theme="vs-dark"
                ariaLabel="Rego policy source code editor"
                ariaDescribedBy={errorId}
                options={{
                  fontSize: 12,
                  minimap: { enabled: false },
                  lineNumbers: 'on',
                  folding: true,
                }}
              />
            </div>
            {/* Validation result */}
            {validation && (
              <div
                className={
                  'mt-2 rounded-md border px-3 py-2 text-xs ' +
                  (validation.valid
                    ? 'border-green-500/30 bg-green-500/5 text-green-400'
                    : 'border-red-500/30 bg-red-500/5 text-red-400')
                }
              >
                <div className="flex items-center gap-2 font-medium">
                  {validation.valid ? (
                    <>
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      <span>Rego syntax is valid</span>
                    </>
                  ) : (
                    <>
                      <AlertCircle className="h-3.5 w-3.5" />
                      <span>Rego syntax errors</span>
                    </>
                  )}
                </div>
                {validation.errors && validation.errors.length > 0 && (
                  <ul className="mt-1 list-disc list-inside space-y-0.5">
                    {validation.errors.map((e, i) => (
                      <li key={i}>{e}</li>
                    ))}
                  </ul>
                )}
                {validation.warnings && validation.warnings.length > 0 && (
                  <ul className="mt-1 list-disc list-inside space-y-0.5 text-yellow-400">
                    {validation.warnings.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>

          {error && (
            <div
              id={errorId}
              role="alert"
              className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-400"
            >
              {error}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-2 px-5 py-3 border-t border-slate-800 bg-slate-900/60">
          <button
            type="button"
            onClick={handleValidate}
            disabled={validating || !regoSource.trim()}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-slate-700 bg-slate-800 hover:bg-slate-700 text-sm text-white disabled:opacity-50 transition-colors"
          >
            {validating ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <CheckCircle2 className="h-4 w-4" />
            )}
            <span>Validate</span>
          </button>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 hover:bg-slate-700 text-sm text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleSave}
              disabled={saving}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 transition-colors"
            >
              {saving ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Save className="h-4 w-4" />
              )}
              <span>{isEdit ? 'Save changes' : 'Create policy'}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default PolicyEditor;
