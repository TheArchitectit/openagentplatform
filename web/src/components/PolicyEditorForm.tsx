// PolicyEditor modal form. See policy-editor.tsx for rationale.

import { useEffect, useId, useState } from 'react';
import { X, FileCode2 } from 'lucide-react';
import type {
  Policy,
  CreatePolicyInput,
  UpdatePolicyInput,
  PolicyValidationResult,
  PolicyCategory,
  PolicySeverity,
  PolicyEnforcement,
} from '@/lib/usePolicies';
import {
  TEMPLATES,
  CATEGORY_OPTIONS,
  SEVERITY_OPTIONS,
  ENFORCEMENT_OPTIONS,
  PolicyEditorProps,
  fieldClasses,
  labelClasses,
} from './policy-editor-helpers';
import { PolicyEditorRegoAndFooter } from './PolicyEditorForm.rego-section';

export function PolicyEditor({ policy, onClose, onSave, validateRego }: PolicyEditorProps) {
  const isEdit = Boolean(policy?.id);

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

        <div className="p-5 space-y-4 max-h-[70vh] overflow-y-auto">
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

          <PolicyEditorRegoAndFooter
            regoSource={regoSource}
            onRegoChange={(v) => {
              setRegoSource(v);
              setValidation(null);
            }}
            validation={validation}
            error={error}
            errorId={errorId}
            regoId={regoId}
            validating={validating}
            saving={saving}
            onValidate={() => void handleValidate()}
            onSave={() => void handleSave()}
            onClose={onClose}
            isEdit={isEdit}
          />
        </div>
      </div>
    </div>
  );
}

export default PolicyEditor;
