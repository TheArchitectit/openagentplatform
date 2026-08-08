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


import { SSOProviderModal, SSORow } from './sso_components'

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
    async (input: CreateSSOProviderInput | UpdateSSOProviderInput) => {
      await createSSOProvider(input as CreateSSOProviderInput);
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

