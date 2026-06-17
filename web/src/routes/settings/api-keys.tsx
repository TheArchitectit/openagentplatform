// Settings — API Keys.
//
// Table of API keys with create modal (scopes + expiry). When a new key is
// created the secret is shown once with a copy-to-clipboard button and a
// prominent warning. Keys can be revoked.

import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useEffect, useState } from 'react';
import {
  Plus,
  X,
  Copy,
  Trash2,
  KeyRound,
  Check,
} from 'lucide-react';
import {
  useSettings,
  API_KEY_SCOPES,
  type CreateAPIKeyInput,
  type APIKeyExpiry,
  type CreateAPIKeyResult,
} from '@/lib/useSettings';

export const Route = createFileRoute('/settings/api-keys')({
  component: APIKeysPage,
});

function keyStatusClasses(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'revoked':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'expired':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function APIKeysPage() {
  const { apiKeys, isLoadingAPIKeys, fetchAPIKeys, createAPIKey, revokeAPIKey } =
    useSettings();

  const [showCreate, setShowCreate] = useState(false);
  const [newKey, setNewKey] = useState<CreateAPIKeyResult | null>(null);

  useEffect(() => {
    fetchAPIKeys();
  }, [fetchAPIKeys]);

  const handleCreate = useCallback(
    async (input: CreateAPIKeyInput) => {
      const res = await createAPIKey(input);
      setShowCreate(false);
      setNewKey(res);
    },
    [createAPIKey]
  );

  const handleRevoke = useCallback(
    async (id: string, name: string) => {
      if (confirm(`Revoke API key "${name}"? This cannot be undone.`)) {
        await revokeAPIKey(id);
      }
    },
    [revokeAPIKey]
  );

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">API Keys</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            Create and manage API keys for programmatic access to the platform.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Plus className="h-4 w-4" />
          Create API Key
        </button>
      </div>

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Name</th>
                <th className="px-4 py-2.5 font-medium">Prefix</th>
                <th className="px-4 py-2.5 font-medium">Scopes</th>
                <th className="px-4 py-2.5 font-medium">Created</th>
                <th className="px-4 py-2.5 font-medium">Expires</th>
                <th className="px-4 py-2.5 font-medium">Last Used</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoadingAPIKeys ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading API keys...
                  </td>
                </tr>
              ) : apiKeys.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
                    No API keys yet. Create one to get started.
                  </td>
                </tr>
              ) : (
                apiKeys.map((k) => (
                  <tr key={k.id} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5 text-white font-medium">
                      <div className="inline-flex items-center gap-1.5">
                        <KeyRound className="h-3 w-3 text-gray-400" />
                        {k.name}
                      </div>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-gray-300">
                      {k.prefix}...
                    </td>
                    <td className="px-4 py-2.5 text-xs text-gray-300">
                      {k.scopes.length} scope{k.scopes.length === 1 ? '' : 's'}
                    </td>
                    <td className="px-4 py-2.5 text-xs text-gray-300">
                      {new Date(k.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-2.5 text-xs text-gray-300">
                      {k.expires_at
                        ? new Date(k.expires_at).toLocaleDateString()
                        : 'Never'}
                    </td>
                    <td className="px-4 py-2.5 text-xs text-gray-300">
                      {k.last_used_at
                        ? new Date(k.last_used_at).toLocaleString()
                        : 'Never'}
                    </td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border ${keyStatusClasses(k.status)}`}
                      >
                        {k.status}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      {k.status === 'active' && (
                        <button
                          type="button"
                          onClick={() => handleRevoke(k.id, k.name)}
                          title="Revoke key"
                          className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-red-600 border border-slate-700 text-xs text-red-400 hover:text-white transition-colors"
                        >
                          <Trash2 className="h-3 w-3" />
                          Revoke
                        </button>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {showCreate && (
        <CreateKeyModal
          onClose={() => setShowCreate(false)}
          onSubmit={handleCreate}
        />
      )}

      {newKey && (
        <KeyRevealModal
          result={newKey}
          onClose={() => setNewKey(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Create modal
// ---------------------------------------------------------------------------

function CreateKeyModal({
  onClose,
  onSubmit,
}: {
  onClose: () => void;
  onSubmit: (input: CreateAPIKeyInput) => Promise<void>;
}) {
  const [name, setName] = useState('');
  const [expiry, setExpiry] = useState<APIKeyExpiry>('90d');
  const [customDate, setCustomDate] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  const toggleScope = useCallback((scope: string, checked: boolean) => {
    setSelectedScopes((prev) => {
      const next = new Set(prev);
      if (checked) next.add(scope);
      else next.delete(scope);
      return next;
    });
  }, []);

  // Group scopes by category
  const groupedScopes = API_KEY_SCOPES.reduce<Record<string, typeof API_KEY_SCOPES>>(
    (acc, scope) => {
      if (!acc[scope.category]) acc[scope.category] = [];
      acc[scope.category].push(scope);
      return acc;
    },
    {}
  );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || selectedScopes.size === 0) return;
    setBusy(true);
    try {
      await onSubmit({
        name: name.trim(),
        scopes: Array.from(selectedScopes),
        expiry,
        custom_expiry: expiry === 'custom' ? customDate : undefined,
      });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-2xl mx-4 max-h-[85vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Create API Key</h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="key-name" className="block text-xs text-gray-300 mb-1">
              Key Name
            </label>
            <input
              id="key-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. CI pipeline"
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>

          <div>
            <label htmlFor="key-expiry" className="block text-xs text-gray-300 mb-1">
              Expiry
            </label>
            <select
              id="key-expiry"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value as APIKeyExpiry)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            >
              <option value="30d">30 days</option>
              <option value="90d">90 days</option>
              <option value="1yr">1 year</option>
              <option value="custom">Custom date</option>
              <option value="never">Never</option>
            </select>
          </div>

          {expiry === 'custom' && (
            <div>
              <label htmlFor="key-custom-date" className="block text-xs text-gray-300 mb-1">
                Expiry Date
              </label>
              <input
                id="key-custom-date"
                type="date"
                value={customDate}
                onChange={(e) => setCustomDate(e.target.value)}
                min={new Date().toISOString().slice(0, 10)}
                className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
              />
            </div>
          )}

          <div>
            <label className="block text-xs text-gray-300 mb-2">
              Scopes ({selectedScopes.size} selected)
            </label>
            <div className="space-y-3">
              {Object.entries(groupedScopes).map(([cat, scopes]) => (
                <div key={cat} className="rounded-md border border-slate-800 bg-slate-800/40 p-3">
                  <div className="text-xs font-semibold text-white uppercase tracking-wider mb-2">
                    {cat}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {scopes.map((s) => {
                      const checked = selectedScopes.has(s.key);
                      return (
                        <label
                          key={s.key}
                          className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md border border-slate-700 bg-slate-800 text-xs text-gray-300 cursor-pointer hover:border-slate-600 transition-colors"
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) => toggleScope(s.key, e.target.checked)}
                            className="h-3 w-3 rounded border-slate-600 text-blue-500 focus:ring-blue-500 focus:ring-offset-0"
                          />
                          {s.label}
                        </label>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="inline-flex items-center px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy || !name.trim() || selectedScopes.size === 0}
              className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {busy ? 'Creating...' : 'Create Key'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Key reveal modal (shown once after creation)
// ---------------------------------------------------------------------------

function KeyRevealModal({
  result,
  onClose,
}: {
  result: CreateAPIKeyResult;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(result.secret).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [result.secret]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-md mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">API Key Created</h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="flex items-center gap-2 rounded-md border border-yellow-500/20 bg-yellow-500/10 px-3 py-2 text-xs text-yellow-400 mb-4">
          <span className="text-base">⚠</span>
          <span>
            Copy and save this key now — it will <strong>not</strong> be shown again.
          </span>
        </div>
        <div className="mb-4">
          <label className="block text-xs text-gray-300 mb-1">Key</label>
          <div className="flex items-center gap-2 rounded-md border border-slate-700 bg-slate-800/60 p-2">
            <span className="flex-1 font-mono text-sm text-white break-all">{result.secret}</span>
            <button
              type="button"
              onClick={handleCopy}
              className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors flex-shrink-0"
            >
              {copied ? (
                <>
                  <Check className="h-3 w-3" /> Copied
                </>
              ) : (
                <>
                  <Copy className="h-3 w-3" /> Copy
                </>
              )}
            </button>
          </div>
        </div>
        <div className="flex items-center justify-end">
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white transition-colors"
          >
            I have saved the key
          </button>
        </div>
      </div>
    </div>
  );
}
