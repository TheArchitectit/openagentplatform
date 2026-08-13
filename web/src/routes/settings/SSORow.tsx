// SSORow — a single SSO provider table row with inline test-connection UI.
//
// Extracted from sso_components.tsx to keep that file focused on the
// SSOProviderModal. Re-exported from sso_components.tsx so callers
// (settings/sso.tsx) can keep importing from the original path.

import { useState } from 'react';
import { Plug, ShieldCheck, Star, Trash2, AlertCircle } from 'lucide-react';
import type {
  SSOSSOProvider,
  SSOTestResult,
} from '@/lib/useSettings';

export function SSORow({
  provider,
  onEdit,
  onDelete,
  onTest,
  onSetDefault,
}: {
  provider: SSOSSOProvider;
  onEdit: () => void;
  onDelete: () => void;
  onTest: () => Promise<SSOTestResult>;
  onSetDefault: () => void;
}) {
  const [testResult, setTestResult] = useState<SSOTestResult | null>(null);
  const [testing, setTesting] = useState(false);

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await onTest();
      setTestResult(res);
    } catch (err) {
      setTestResult({ success: false, message: (err as Error).message });
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <tr className="hover:bg-slate-800/40 transition-colors">
        <td className="px-4 py-2.5 text-white font-medium">
          <div className="inline-flex items-center gap-1.5">
            <Plug className="h-3 w-3 text-gray-400" />
            {provider.name}
          </div>
        </td>
        <td className="px-4 py-2.5">
          <span className="inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border bg-slate-500/10 text-gray-300 border-slate-500/20">
            {provider.type.toUpperCase()}
          </span>
        </td>
        <td className="px-4 py-2.5 text-xs text-gray-300">
          {provider.domain_whitelist.length > 0
            ? provider.domain_whitelist.join(', ')
            : 'All domains'}
        </td>
        <td className="px-4 py-2.5">
          <span
            className={`inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border ${providerStatusClasses(provider.status)}`}
          >
            {provider.status}
          </span>
        </td>
        <td className="px-4 py-2.5">
          {provider.is_default ? (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border bg-slate-500/10 text-gray-300 border-slate-500/20">
              <Star className="h-3 w-3" /> default
            </span>
          ) : (
            <button
              type="button"
              onClick={onSetDefault}
              className="inline-flex items-center h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors"
            >
              Set default
            </button>
          )}
        </td>
        <td className="px-4 py-2.5 text-right">
          <div className="flex items-center justify-end gap-1.5">
            <button
              type="button"
              onClick={handleTest}
              disabled={testing}
              className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white disabled:opacity-50 transition-colors"
            >
              <ShieldCheck className="h-3 w-3" /> {testing ? 'Testing...' : 'Test'}
            </button>
            <button
              type="button"
              onClick={onEdit}
              className="inline-flex items-center h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors"
            >
              Edit
            </button>
            <button
              type="button"
              onClick={onDelete}
              className="inline-flex items-center justify-center h-7 w-7 rounded-md bg-slate-800 hover:bg-red-600 border border-slate-700 text-red-400 hover:text-white transition-colors"
            >
              <Trash2 className="h-3 w-3" />
            </button>
          </div>
        </td>
      </tr>
      {testResult && (
        <tr>
          <td colSpan={6} className="px-4 pb-3">
            <div
              className={`flex items-center gap-1.5 rounded-md border px-3 py-2 text-xs ${
                testResult.success
                  ? 'border-green-500/20 bg-green-500/10 text-green-400'
                  : 'border-red-500/20 bg-red-500/10 text-red-400'
              }`}
            >
              {testResult.success ? (
                <span className="inline-flex items-center gap-1.5">
                  <ShieldCheck className="h-3 w-3" /> Connection successful
                  {testResult.latency_ms != null && ` (${testResult.latency_ms}ms)`}
                </span>
              ) : (
                <span className="inline-flex items-center gap-1.5">
                  <AlertCircle className="h-3 w-3" /> {testResult.message}
                </span>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function providerStatusClasses(status: string): string {
  return PROVIDER_STATUS_CLASSES[status] ?? PROVIDER_STATUS_CLASSES.inactive;
}

const PROVIDER_STATUS_CLASSES: Record<string, string> = {
  active: 'bg-green-100 text-green-800 border-green-200',
  inactive: 'bg-gray-100 text-gray-800 border-gray-200',
  error: 'bg-red-100 text-red-800 border-red-200',
};
