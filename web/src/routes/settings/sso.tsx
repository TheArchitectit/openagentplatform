// Settings — SSO.
//
// Configure OIDC and SAML identity providers. Supports test connection,
// default-provider toggle, and full provider CRUD.

import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useEffect, useState } from 'react';
import {
  Plus,
  X,
  Plug,
  Trash2,
  Star,
  ShieldCheck,
  AlertCircle,
} from 'lucide-react';
import {
  useSettings,
  type CreateSSOProviderInput,
  type SSOSSOProvider,
  type SSOProviderType,
  type SSOTestResult,
  type UpdateSSOProviderInput,
} from '@/lib/useSettings';
import './settings.css';

export const Route = createFileRoute('/settings/sso')({
  component: SSOPage,
});

function SSOPage() {
  const {
    ssoProviders,
    isLoadingSSO,
    fetchSSOProviders,
    createSSOProvider,
    updateSSOProvider,
    deleteSSOProvider,
    testSSOConnection,
  } = useSettings();

  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<SSOSSOProvider | null>(null);

  useEffect(() => {
    fetchSSOProviders();
  }, [fetchSSOProviders]);

  const handleCreate = useCallback(
    async (input: CreateSSOProviderInput) => {
      await createSSOProvider(input);
      setShowCreate(false);
    },
    [createSSOProvider]
  );

  const handleUpdate = useCallback(
    async (id: string, input: UpdateSSOProviderInput) => {
      await updateSSOProvider(id, input);
      setEditing(null);
    },
    [updateSSOProvider]
  );

  const handleDelete = useCallback(
    async (id: string, name: string) => {
      if (confirm(`Delete SSO provider "${name}"? Users signed in via this provider will lose access.`)) {
        await deleteSSOProvider(id);
      }
    },
    [deleteSSOProvider]
  );

  const handleTest = useCallback(
    async (id: string) => {
      return testSSOConnection(id);
    },
    [testSSOConnection]
  );

  const handleSetDefault = useCallback(
    async (id: string) => {
      await updateSSOProvider(id, { is_default: true });
    },
    [updateSSOProvider]
  );

  return (
    <>
      <div className="settings-page-header">
        <div>
          <h1>SSO</h1>
          <p>Configure single sign-on providers for your organization.</p>
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
          Add Provider
        </button>
      </div>

      <div className="settings-table-wrap">
        <table className="settings-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Domain(s)</th>
              <th>Status</th>
              <th>Default</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoadingSSO ? (
              <tr className="empty-row">
                <td colSpan={6}>Loading providers...</td>
              </tr>
            ) : ssoProviders.length === 0 ? (
              <tr className="empty-row">
                <td colSpan={6}>No SSO providers configured.</td>
              </tr>
            ) : (
              ssoProviders.map((p) => (
                <SSORow
                  key={p.id}
                  provider={p}
                  onEdit={() => setEditing(p)}
                  onDelete={() => handleDelete(p.id, p.name)}
                  onTest={() => handleTest(p.id)}
                  onSetDefault={() => handleSetDefault(p.id)}
                />
              ))
            )}
          </tbody>
        </table>
      </div>

      {showCreate && (
        <SSOProviderModal
          onClose={() => setShowCreate(false)}
          onSubmit={handleCreate}
        />
      )}

      {editing && (
        <SSOProviderModal
          provider={editing}
          onClose={() => setEditing(null)}
          onSubmit={(input) => handleUpdate(editing.id, input as UpdateSSOProviderInput)}
        />
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Row with inline test result
// ---------------------------------------------------------------------------

function SSORow({
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
      <tr>
        <td style={{ color: 'rgb(241 245 249)', fontWeight: 500 }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.375rem' }}>
            <Plug className="h-3 w-3 text-text-muted" />
            {provider.name}
          </span>
        </td>
        <td>
          <span className="settings-badge settings-badge--built-in">
            {provider.type.toUpperCase()}
          </span>
        </td>
        <td style={{ fontSize: '0.75rem', color: 'rgb(148 163 184)' }}>
          {provider.domain_whitelist.length > 0
            ? provider.domain_whitelist.join(', ')
            : 'All domains'}
        </td>
        <td>
          <span className={`settings-badge settings-badge--${provider.status === 'active' ? 'active' : provider.status === 'error' ? 'failure' : 'inactive'}`}>
            {provider.status}
          </span>
        </td>
        <td>
          {provider.is_default ? (
            <span className="settings-badge settings-badge--built-in">
              <Star className="h-3 w-3" /> default
            </span>
          ) : (
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
                fontSize: '0.75rem',
              }}
              onClick={onSetDefault}
            >
              Set default
            </button>
          )}
        </td>
        <td>
          <div style={{ display: 'flex', gap: '0.375rem' }}>
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
              onClick={handleTest}
              disabled={testing}
            >
              <ShieldCheck className="h-3 w-3" /> {testing ? 'Testing...' : 'Test'}
            </button>
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
                fontSize: '0.75rem',
              }}
              onClick={onEdit}
            >
              Edit
            </button>
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
                fontSize: '0.75rem',
                color: 'rgb(252 165 165)',
              }}
              onClick={onDelete}
            >
              <Trash2 className="h-3 w-3" />
            </button>
          </div>
        </td>
      </tr>
      {testResult && (
        <tr>
          <td colSpan={6} style={{ padding: 0 }}>
            <div
              className={`settings-sso-test ${testResult.success ? 'settings-sso-test--success' : 'settings-sso-test--failure'}`}
              style={{ margin: '0 0.75rem 0.5rem' }}
            >
              {testResult.success ? (
                <span>
                  <ShieldCheck className="h-3 w-3 inline" /> Connection successful
                  {testResult.latency_ms != null && ` (${testResult.latency_ms}ms)`}
                </span>
              ) : (
                <span>
                  <AlertCircle className="h-3 w-3 inline" /> {testResult.message}
                </span>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// SSO provider modal (create / edit)
// ---------------------------------------------------------------------------

function SSOProviderModal({
  provider,
  onClose,
  onSubmit,
}: {
  provider?: SSOSSOProvider;
  onClose: () => void;
  onSubmit: (input: CreateSSOProviderInput | UpdateSSOProviderInput) => Promise<void>;
}) {
  const isEdit = !!provider;
  const [type, setType] = useState<SSOProviderType>(provider?.type ?? 'oidc');
  const [name, setName] = useState(provider?.name ?? '');
  const [issuerUrl, setIssuerUrl] = useState(provider?.issuer_url ?? '');
  const [clientId, setClientId] = useState(provider?.client_id ?? '');
  const [clientSecret, setClientSecret] = useState('');
  const [domains, setDomains] = useState(provider?.domain_whitelist.join(', ') ?? '');
  const [attrMapping, setAttrMapping] = useState<Record<string, string>>(
    provider?.attribute_mapping ?? { email: 'email', name: 'name' }
  );
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !issuerUrl.trim() || !clientId.trim()) return;
    setBusy(true);
    const domainList = domains
      .split(',')
      .map((d) => d.trim())
      .filter(Boolean);
    try {
      if (isEdit) {
        const update: UpdateSSOProviderInput = {
          name: name.trim(),
          issuer_url: issuerUrl.trim(),
          client_id: clientId.trim(),
          domain_whitelist: domainList,
          attribute_mapping: attrMapping,
        };
        if (clientSecret.trim()) update.client_secret = clientSecret.trim();
        await onSubmit(update);
      } else {
        await onSubmit({
          name: name.trim(),
          type,
          issuer_url: issuerUrl.trim(),
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
          domain_whitelist: domainList,
          attribute_mapping: attrMapping,
        });
      }
    } finally {
      setBusy(false);
    }
  };

  const updateAttr = (key: string, value: string) => {
    setAttrMapping((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal settings-modal--wide" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>{isEdit ? `Edit Provider: ${provider!.name}` : 'Add SSO Provider'}</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          {!isEdit && (
            <div className="settings-form-group">
              <label className="settings-form-label" htmlFor="sso-type">
                Type
              </label>
              <select
                id="sso-type"
                className="settings-select"
                value={type}
                onChange={(e) => setType(e.target.value as SSOProviderType)}
              >
                <option value="oidc">OIDC</option>
                <option value="saml">SAML</option>
              </select>
            </div>
          )}
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="sso-name">
              Provider Name
            </label>
            <input
              id="sso-name"
              type="text"
              className="settings-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="sso-issuer">
              Issuer URL
            </label>
            <input
              id="sso-issuer"
              type="url"
              className="settings-input"
              value={issuerUrl}
              onChange={(e) => setIssuerUrl(e.target.value)}
              placeholder="https://accounts.example.com"
              required
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="sso-client">
              Client ID
            </label>
            <input
              id="sso-client"
              type="text"
              className="settings-input"
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              required
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="sso-secret">
              Client Secret
            </label>
            <input
              id="sso-secret"
              type="password"
              className="settings-input"
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
              placeholder={isEdit ? '••••••••  (unchanged)' : ''}
              required={!isEdit}
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="sso-domains">
              Domain Whitelist (comma-separated)
            </label>
            <input
              id="sso-domains"
              type="text"
              className="settings-input"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              placeholder="example.com, example.org"
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label">Attribute Mapping</label>
            {Object.entries(attrMapping).map(([k, v]) => (
              <div
                key={k}
                style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.375rem', alignItems: 'center' }}
              >
                <input
                  type="text"
                  className="settings-input"
                  value={k}
                  readOnly
                  style={{ flex: '0 0 7rem' }}
                />
                <span style={{ color: 'rgb(100 116 139)' }}>→</span>
                <input
                  type="text"
                  className="settings-input"
                  value={v}
                  onChange={(e) => updateAttr(k, e.target.value)}
                />
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
              disabled={busy}
            >
              {busy ? 'Saving...' : isEdit ? 'Save Changes' : 'Add Provider'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
