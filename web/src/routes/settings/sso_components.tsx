// Settings — SSO.
//
// Configure OIDC and SAML identity providers. Supports test connection,
// default-provider toggle, and full provider CRUD.

import { useState } from 'react';
import {
  Plus,
  X,
} from 'lucide-react';
import {
  type CreateSSOProviderInput,
  type SSOSSOProvider,
  type SSOProviderType,
  type SSOTestResult,
  type UpdateSSOProviderInput,
} from '@/lib/useSettings';
import { SSORow } from './SSORow';

// Re-export SSORow for callers importing from this path (settings/sso.tsx).
export { SSORow } from './SSORow';

// ---------------------------------------------------------------------------
// SSO provider modal (create / edit)
// ---------------------------------------------------------------------------

export function SSOProviderModal({
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
