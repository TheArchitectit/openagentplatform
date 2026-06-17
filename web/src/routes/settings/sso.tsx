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

export const Route = createFileRoute('/settings/sso')({
  component: SSOPage,
});

function providerStatusClasses(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'error':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

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
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">SSO</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            Configure single sign-on providers for your organization.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Plus className="h-4 w-4" />
          Add Provider
        </button>
      </div>

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Name</th>
                <th className="px-4 py-2.5 font-medium">Type</th>
                <th className="px-4 py-2.5 font-medium">Domain(s)</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Default</th>
                <th className="px-4 py-2.5 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoadingSSO ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading providers...
                  </td>
                </tr>
              ) : ssoProviders.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    No SSO providers configured.
                  </td>
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
    </div>
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-2xl mx-4 max-h-[85vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">
            {isEdit ? `Edit Provider: ${provider!.name}` : 'Add SSO Provider'}
          </h2>
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
          {!isEdit && (
            <div>
              <label htmlFor="sso-type" className="block text-xs text-gray-300 mb-1">
                Type
              </label>
              <select
                id="sso-type"
                value={type}
                onChange={(e) => setType(e.target.value as SSOProviderType)}
                className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
              >
                <option value="oidc">OIDC</option>
                <option value="saml">SAML</option>
              </select>
            </div>
          )}
          <div>
            <label htmlFor="sso-name" className="block text-xs text-gray-300 mb-1">
              Provider Name
            </label>
            <input
              id="sso-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="sso-issuer" className="block text-xs text-gray-300 mb-1">
              Issuer URL
            </label>
            <input
              id="sso-issuer"
              type="url"
              value={issuerUrl}
              onChange={(e) => setIssuerUrl(e.target.value)}
              placeholder="https://accounts.example.com"
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="sso-client" className="block text-xs text-gray-300 mb-1">
              Client ID
            </label>
            <input
              id="sso-client"
              type="text"
              value={clientId}
              onChange={(e) => setClientId(e.target.value)}
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="sso-secret" className="block text-xs text-gray-300 mb-1">
              Client Secret
            </label>
            <input
              id="sso-secret"
              type="password"
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
              placeholder={isEdit ? '••••••••  (unchanged)' : ''}
              required={!isEdit}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="sso-domains" className="block text-xs text-gray-300 mb-1">
              Domain Whitelist (comma-separated)
            </label>
            <input
              id="sso-domains"
              type="text"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              placeholder="example.com, example.org"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label className="block text-xs text-gray-300 mb-1">Attribute Mapping</label>
            <div className="space-y-1.5">
              {Object.entries(attrMapping).map(([k, v]) => (
                <div key={k} className="flex items-center gap-2">
                  <input
                    type="text"
                    value={k}
                    readOnly
                    className="w-28 h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white cursor-not-allowed"
                  />
                  <span className="text-gray-400">→</span>
                  <input
                    type="text"
                    value={v}
                    onChange={(e) => updateAttr(k, e.target.value)}
                    className="flex-1 h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
                  />
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
              disabled={busy}
              className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {busy ? 'Saving...' : isEdit ? 'Save Changes' : 'Add Provider'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
