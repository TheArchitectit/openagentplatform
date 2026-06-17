import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import {
  ShieldCheck,
  Plus,
  Search,
  Lock,
  Eye,
  FileText,
  RefreshCw,
  CircleCheck,
  CircleAlert,
  CircleX,
} from 'lucide-react';
import { usePolicies, type Policy, type PolicyCategory, type PolicyEnforcement } from '@/lib/usePolicies';
import { PolicyEditor } from '@/components/policy-editor';
import { SeverityBadge } from '@/components/severity-badge';

export const Route = createFileRoute('/policies/')({
  component: PoliciesListPage,
});

type CategoryFilter = 'all' | PolicyCategory;
type EnforcementFilter = 'all' | PolicyEnforcement;

const CATEGORY_OPTIONS: { value: CategoryFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'security', label: 'Security' },
  { value: 'compliance', label: 'Compliance' },
  { value: 'configuration', label: 'Configuration' },
  { value: 'performance', label: 'Performance' },
  { value: 'custom', label: 'Custom' },
];

const ENFORCEMENT_OPTIONS: { value: EnforcementFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'enforce', label: 'Enforce' },
  { value: 'audit', label: 'Audit' },
  { value: 'report', label: 'Report' },
];

function categoryClasses(cat: PolicyCategory): string {
  switch (cat) {
    case 'security':
      return 'bg-red-500/10 text-red-400 border-red-800';
    case 'compliance':
      return 'bg-blue-500/10 text-blue-400 border-blue-800';
    case 'configuration':
      return 'bg-blue-600/10 text-blue-400 border-blue-500/20';
    case 'performance':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-800';
    case 'custom':
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-700';
  }
}

function enforcementIcon(mode: PolicyEnforcement) {
  switch (mode) {
    case 'enforce':
      return Lock;
    case 'audit':
      return Eye;
    case 'report':
    default:
      return FileText;
  }
}

function enforcementClasses(mode: PolicyEnforcement): string {
  switch (mode) {
    case 'enforce':
      return 'bg-red-500/10 text-red-400 border-red-800';
    case 'audit':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-800';
    case 'report':
    default:
      return 'bg-blue-500/10 text-blue-400 border-blue-800';
  }
}

function complianceColor(pct: number | undefined): string {
  if (pct === undefined || pct === null) return 'text-gray-400';
  if (pct >= 80) return 'text-green-400';
  if (pct >= 60) return 'text-yellow-400';
  return 'text-red-400';
}

function complianceIcon(pct: number | undefined) {
  if (pct === undefined || pct === null) return CircleX;
  if (pct >= 80) return CircleCheck;
  if (pct >= 60) return CircleAlert;
  return CircleX;
}

function PoliciesListPage() {
  const navigate = useNavigate();
  const {
    policies,
    isLoading,
    error,
    refresh,
    createPolicy,
    updatePolicy,
    validatePolicy,
  } = usePolicies();

  const [category, setCategory] = useState<CategoryFilter>('all');
  const [enforcement, setEnforcement] = useState<EnforcementFilter>('all');
  const [query, setQuery] = useState('');
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<Policy | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return policies.filter((p) => {
      if (category !== 'all' && p.category !== category) return false;
      if (enforcement !== 'all' && p.enforcement !== enforcement) return false;
      if (q) {
        const hay = `${p.name} ${p.description ?? ''}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [policies, category, enforcement, query]);

  const counts = useMemo(() => {
    const c: Record<CategoryFilter, number> = {
      all: policies.length,
      security: 0,
      compliance: 0,
      configuration: 0,
      performance: 0,
      custom: 0,
    };
    for (const p of policies) c[p.category] = (c[p.category] ?? 0) + 1;
    return c;
  }, [policies]);

  const handleSave = async (
    input:
      | Parameters<typeof createPolicy>[0]
      | { id: string; input: Parameters<typeof updatePolicy>[1] }
  ) => {
    setSaving(true);
    setSaveError(null);
    try {
      if ('id' in input) {
        await updatePolicy(input.id, input.input);
      } else {
        await createPolicy(input);
      }
      setEditorOpen(false);
      setEditingPolicy(null);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Save failed');
      throw err;
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
            <ShieldCheck className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Policy Library</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Rego-based compliance and security policies for your fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            aria-label="Refresh policies"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => {
              setEditingPolicy(null);
              setEditorOpen(true);
            }}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            <span>Create Policy</span>
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="space-y-3">
        <div
          role="tablist"
          aria-label="Filter policies by category"
          className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto"
        >
          {CATEGORY_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={category === opt.value}
              onClick={() => setCategory(opt.value)}
              className={
                'px-3 h-8 rounded text-sm transition-colors whitespace-nowrap focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ' +
                (category === opt.value
                  ? 'bg-slate-800 text-white'
                  : 'text-gray-300 hover:text-white')
              }
            >
              {opt.label}
              <span className="ml-2 text-xs text-gray-400" aria-hidden="true">{counts[opt.value] ?? 0}</span>
              <span className="sr-only">({counts[opt.value] ?? 0} policies)</span>
            </button>
          ))}
        </div>

        <div className="flex items-center justify-between flex-wrap gap-3">
          <div
            role="group"
            aria-label="Enforcement mode"
            className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800"
          >
            {ENFORCEMENT_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                aria-pressed={enforcement === opt.value}
                onClick={() => setEnforcement(opt.value)}
                className={
                  'px-3 h-8 rounded text-sm transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ' +
                  (enforcement === opt.value
                    ? 'bg-slate-800 text-white'
                    : 'text-gray-300 hover:text-white')
                }
              >
                {opt.label}
              </button>
            ))}
          </div>

          <div className="relative w-full sm:w-72" role="search">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
            <input
              type="search"
              role="searchbox"
              aria-label="Search policies"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search policies…"
              className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
        </div>
      </div>

      {saveError && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {saveError}
        </div>
      )}

      {/* Card grid */}
      {isLoading && policies.length === 0 ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status" aria-live="polite">
          Loading policies…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-800 bg-red-500/5 p-12 text-center text-red-400" role="alert">
          Failed to load policies: {error.message}
        </div>
      ) : filtered.length === 0 ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status">
          No policies match the current filters.
        </div>
      ) : (
        <div role="list" aria-label="Policy list" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((p) => {
            const EnforceIcon = enforcementIcon(p.enforcement);
            const ComplIcon = complianceIcon(p.compliance_pct);
            return (
              <button
                key={p.id}
                type="button"
                role="listitem"
                aria-label={`Policy: ${p.name}. Category: ${p.category}. Enforcement: ${p.enforcement}. ${p.compliance_pct !== undefined && p.compliance_pct !== null ? `${p.compliance_pct.toFixed(0)} percent compliant` : ''}`}
                onClick={() => {
                  void navigate({ to: '/policies/$policyId', params: { policyId: p.id } });
                }}
                className="text-left rounded-lg border border-slate-800 bg-slate-900 p-5 hover:border-slate-700 hover:bg-slate-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
              >
                <div className="flex items-start justify-between mb-3">
                  <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center shrink-0" aria-hidden="true">
                    <ShieldCheck className="h-4 w-4 text-blue-400" />
                  </div>
                  <div className="flex items-center gap-2">
                    {!p.enabled && (
                      <span className="inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border bg-slate-500/10 text-gray-400 border-slate-700" role="status" aria-label="Status: disabled">
                        Disabled
                      </span>
                    )}
                  </div>
                </div>

                <h3 className="text-sm font-semibold text-white truncate">{p.name}</h3>
                {p.description && (
                  <p className="text-xs text-gray-300 mt-1 line-clamp-2">{p.description}</p>
                )}

                <div className="flex flex-wrap items-center gap-2 mt-3">
                  <span
                    className={
                      'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                      categoryClasses(p.category)
                    }
                    aria-label={`Category: ${p.category}`}
                  >
                    {p.category}
                  </span>
                  <SeverityBadge severity={p.severity} />
                  <span
                    className={
                      'inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border ' +
                      enforcementClasses(p.enforcement)
                    }
                    aria-label={`Enforcement: ${p.enforcement}`}
                  >
                    <EnforceIcon className="h-2.5 w-2.5" aria-hidden="true" />
                    <span className="capitalize">{p.enforcement}</span>
                  </span>
                </div>

                <div className="mt-4 pt-3 border-t border-slate-800 flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-gray-300">
                    <ComplIcon className={'h-3.5 w-3.5 ' + complianceColor(p.compliance_pct)} aria-hidden="true" />
                    <span className={complianceColor(p.compliance_pct)}>
                      {p.compliance_pct !== undefined && p.compliance_pct !== null
                        ? `${p.compliance_pct.toFixed(0)}%`
                        : '—'}
                    </span>
                    <span className="text-gray-400">compliant</span>
                  </div>
                  <div className="text-gray-400">
                    {p.agent_count !== undefined ? `${p.agent_count} agent${p.agent_count === 1 ? '' : 's'}` : '—'}
                  </div>
                </div>
              </button>
            );
          })}
        </div>
      )}

      {/* Editor modal */}
      {editorOpen && (
        <PolicyEditor
          policy={editingPolicy}
          onClose={() => {
            if (saving) return;
            setEditorOpen(false);
            setEditingPolicy(null);
          }}
          onSave={handleSave}
          validateRego={validatePolicy}
        />
      )}
    </div>
  );
}
