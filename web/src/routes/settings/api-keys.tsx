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
import '../settings.css';

export const Route = createFileRoute('/settings/api-keys')({
  component: APIKeysPage,
});

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
    <>
      <div className="settings-page-header">
        <div>
          <h1>API Keys</h1>
          <p>Create and manage API keys for programmatic access to the platform.</p>
        </div>
        <button
          type="button"
          className="settings-input"
          style={{
            width: 'auto',
            height: '2.25rem',
            padding: '0 0.75rem',
            cursor: 'pointer',
            display: 'inline-flex',
            alignItems: 'center',
            gap: '0.375rem',
            background: 'rgb(99 102 241)',
            color: 'white',
            border: 'none',
            fontWeight: 500,
          }}
          onClick={() => setShowCreate(true)}
        >
          <Plus className="h-4 w-4" />
          Create API Key
        </button>
      </div>

      <div className="settings-table-wrap">
        <table className="settings-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Prefix</th>
              <th>Scopes</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Last Used</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoadingAPIKeys ? (
              <tr className="empty-row">
                <td colSpan={8}>Loading API keys...</td>
              </tr>
            ) : apiKeys.length === 0 ? (
              <tr className="empty-row">
                <td colSpan={8}>No API keys yet. Create one to get started.</td>
              </tr>
            ) : (
              apiKeys.map((k) => (
                <tr key={k.id}>
                  <td style={{ color: 'rgb(241 245 249)', fontWeight: 500 }}>
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.375rem' }}>
                      <KeyRound className="h-3 w-3 text-text-muted" />
                      {k.name}
                    </span>
                  </td>
                  <td style={{ fontFamily: 'monospace', fontSize: '0.75rem' }}>
                    {k.prefix}...
                  </td>
                  <td style={{ fontSize: '0.75rem', color: 'rgb(148 163 184)' }}>
                    {k.scopes.length} scope{k.scopes.length === 1 ? '' : 's'}
                  </td>
                  <td>{new Date(k.created_at).toLocaleDateString()}</td>
                  <td>
                    {k.expires_at
                      ? new Date(k.expires_at).toLocaleDateString()
                      : 'Never'}
                  </td>
                  <td>
                    {k.last_used_at
                      ? new Date(k.last_used_at).toLocaleString()
                      : 'Never'}
                  </td>
                  <td>
                    <span className={`settings-badge settings-badge--${k.status}`}>
                      {k.status}
                    </span>
                  </td>
                  <td>
                    {k.status === 'active' && (
                      <button
                        type="button"
                        className="settings-input"
                        style={{
                          width: 'auto',
                          height: '1.75rem',
                          padding: '0 0.5rem',
                          cursor: 'pointer',
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: '0.25rem',
                          fontSize: '0.75rem',
                          color: 'rgb(252 165 165)',
                        }}
                        onClick={() => handleRevoke(k.id, k.name)}
                        title="Revoke key"
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
    </>
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
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal settings-modal--wide" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>Create API Key</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="key-name">
              Key Name
            </label>
            <input
              id="key-name"
              type="text"
              className="settings-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. CI pipeline"
              required
            />
          </div>

          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="key-expiry">
              Expiry
            </label>
            <select
              id="key-expiry"
              className="settings-select"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value as APIKeyExpiry)}
            >
              <option value="30d">30 days</option>
              <option value="90d">90 days</option>
              <option value="1yr">1 year</option>
              <option value="custom">Custom date</option>
              <option value="never">Never</option>
            </select>
          </div>

          {expiry === 'custom' && (
            <div className="settings-form-group">
              <label className="settings-form-label" htmlFor="key-custom-date">
                Expiry Date
              </label>
              <input
                id="key-custom-date"
                type="date"
                className="settings-input"
                value={customDate}
                onChange={(e) => setCustomDate(e.target.value)}
                min={new Date().toISOString().slice(0, 10)}
              />
            </div>
          )}

          <div className="settings-form-group">
            <label className="settings-form-label">
              Scopes ({selectedScopes.size} selected)
            </label>
            {Object.entries(groupedScopes).map(([cat, scopes]) => (
              <div key={cat} className="settings-perm-category">
                <div className="settings-perm-category-header">
                  <span className="settings-perm-category-label">{cat}</span>
                </div>
                <div className="settings-perm-row">
                  {scopes.map((s) => (
                    <label key={s.key} className="settings-perm-check">
                      <input
                        type="checkbox"
                        checked={selectedScopes.has(s.key)}
                        onChange={(e) => toggleScope(s.key, e.target.checked)}
                      />
                      {s.label}
                    </label>
                  ))}
                </div>
              </div>
            ))}
          </div>

          <div className="settings-form-actions">
            <button
              type="button"
              className="settings-input"
              style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer' }}
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="settings-input"
              style={{
                width: 'auto',
                height: '2.25rem',
                padding: '0 0.75rem',
                cursor: 'pointer',
                background: 'rgb(99 102 241)',
                color: 'white',
                border: 'none',
                fontWeight: 500,
              }}
              disabled={busy || !name.trim() || selectedScopes.size === 0}
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
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>API Key Created</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="settings-key-reveal">
          <span style={{ fontSize: '1.25rem' }}>⚠</span>
          <span>
            Copy and save this key now — it will <strong>not</strong> be shown again.
          </span>
        </div>
        <div className="settings-form-group">
          <label className="settings-form-label">Key</label>
          <div className="settings-key-value">
            <span style={{ flex: 1 }}>{result.secret}</span>
            <button
              type="button"
              className="settings-input"
              style={{
                width: 'auto',
                height: '1.75rem',
                padding: '0 0.5rem',
                cursor: 'pointer',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
                fontSize: '0.75rem',
              }}
              onClick={handleCopy}
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
        <div className="settings-form-actions">
          <button
            type="button"
            className="settings-input"
            style={{
              width: 'auto',
              height: '2.25rem',
              padding: '0 0.75rem',
              cursor: 'pointer',
              background: 'rgb(99 102 241)',
              color: 'white',
              border: 'none',
              fontWeight: 500,
            }}
            onClick={onClose}
          >
            I have saved the key
          </button>
        </div>
      </div>
    </div>
  );
}
