// Rego editor + validation-result + footer section for PolicyEditorForm.
// Extracted from PolicyEditorForm.tsx to keep that file under the size gate.

import { Loader2, CheckCircle2, AlertCircle, Save, X } from 'lucide-react';
import type { PolicyValidationResult } from '@/lib/usePolicies';
import { MonacoEditor } from '@/components/monaco-editor';
import { fieldClasses, labelClasses } from './policy-editor-helpers';

export interface PolicyEditorRegoAndFooterProps {
  regoSource: string;
  onRegoChange: (v: string) => void;
  validation: PolicyValidationResult | null;
  error: string | null;
  errorId: string;
  regoId: string;
  validating: boolean;
  saving: boolean;
  onValidate: () => void;
  onSave: () => void;
  onClose: () => void;
  isEdit: boolean;
}

export function PolicyEditorRegoAndFooter({
  regoSource,
  onRegoChange,
  validation,
  error,
  errorId,
  regoId,
  validating,
  saving,
  onValidate,
  onSave,
  onClose,
  isEdit,
}: PolicyEditorRegoAndFooterProps) {
  return (
    <>
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
              onRegoChange(v);
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

      {/* Footer */}
      <div className="flex items-center justify-between gap-2 px-5 py-3 border-t border-slate-800 bg-slate-900/60">
        <button
          type="button"
          onClick={onValidate}
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
            onClick={onSave}
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
    </>
  );
}
