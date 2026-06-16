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
      return 'bg-rose-500/10 text-rose-300 border-rose-500/20';
    case 'compliance':
      return 'bg-sky-500/10 text-sky-300 border-sky-500/20';
    case 'configuration':
      return 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20';
    case 'performance':
      return 'bg-amber-500/10 text-amber-300 border-amber-500/20';
    case 'custom':
    default:
      return 'bg-slate-500/10 text-slate-300 border-slate-500/20';
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
      return 'bg-rose-500/10 text-rose-300 border-rose-500/20';
    case 'audit':
      return 'bg-amber-500/10 text-amber-300 border-amber-500/20';
    case 'report':
    default:
      return 'bg-sky-500/10 text-sky-300 border-sky-500/20';
  }
}

function complianceColor(pct: number | undefined): string {
  if (pct === undefined || pct === null) return 'text-slate-500';
  if (pct >= 80) return 'text-emerald-400';
  if (pct >= 60) return 'text-amber-400';
  return 'text-rose-400';
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
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <ShieldCheck className="h-4 w-4 text-slate-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Policy Library</h1>
            <p className="text-slate-400 text-sm mt-0.5">
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
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-slate-200 disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => {
              setEditingPolicy(null);
              setEditorOpen(true);
            }}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white transition-colors"
          >
            <Plus className="h-4 w-4" />
            <span>Create Policy</span>
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="space-y-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto">
          {CATEGORY_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              onClick={() => setCategory(opt.value)}
              className={
                'px-3 h-8 rounded text-sm transition-colors whitespace-nowrap ' +
                (category === opt.value
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:text-slate-200')
              }
            >
              {opt.label}
              <span className="ml-2 text-xs text-slate-500">{counts[opt.value] ?? 0}</span>
            </button>
          ))}
        </div>

        <div className="flex items-center justify-between flex-wrap gap-3">
          <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800">
            {ENFORCEMENT_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setEnforcement(opt.value)}
                className={
                  'px-3 h-8 rounded text-sm transition-colors ' +
                  (enforcement === opt.value
                    ? 'bg-slate-800 text-slate-100'
                    : 'text-slate-400 hover:text-slate-200')
                }
              >
                {opt.label}
              </button>
            ))}
          </div>

          <div className="relative w-full sm:w-72">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search policies…"
              className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
          </div>
        </div>
      </div>

      {saveError && (
        <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
          {saveError}
        </div>
      )}

      {/* Card grid */}
      {isLoading && policies.length === 0 ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
          Loading policies…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-rose-500/30 bg-rose-500/5 p-12 text-center text-rose-300">
          Failed to load policies: {error.message}
        </div>
      ) : filtered.length === 0 ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
          No policies match the current filters.
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((p) => {
            const EnforceIcon = enforcementIcon(p.enforcement);
            const ComplIcon = complianceIcon(p.compliance_pct);
            return (
              <button
                key={p.id}
                type="button"
                onClick={() => {
                  void navigate({ to: '/policies/$policyId', params: { policyId: p.id } });
                }}
                className="text-left rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 hover:bg-slate-900 transition-colors"
              >
                <div className="flex items-start justify-between mb-3">
                  <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center shrink-0">
                    <ShieldCheck className="h-4 w-4 text-indigo-400" />
                  </div>
                  <div className="flex items-center gap-2">
                    {!p.enabled && (
                      <span className="inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border bg-slate-500/10 text-slate-400 border-slate-500/20">
                        Disabled
                      </span>
                    )}
                  </div>
                </div>

                <h3 className="text-sm font-semibold text-slate-100 truncate">{p.name}</h3>
                {p.description && (
                  <p className="text-xs text-slate-400 mt-1 line-clamp-2">{p.description}</p>
                )}

                <div className="flex flex-wrap items-center gap-2 mt-3">
                  <span
                    className={
                      'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                      categoryClasses(p.category)
                    }
                  >
                    {p.category}
                  </span>
                  <SeverityBadge severity={p.severity} />
                  <span
                    className={
                      'inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border ' +
                      enforcementClasses(p.enforcement)
                    }
                  >
                    <EnforceIcon className="h-2.5 w-2.5" />
                    <span className="capitalize">{p.enforcement}</span>
                  </span>
                </div>

                <div className="mt-4 pt-3 border-t border-slate-800 flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-slate-400">
                    <ComplIcon className={'h-3.5 w-3.5 ' + complianceColor(p.compliance_pct)} />
                    <span className={complianceColor(p.compliance_pct)}>
                      {p.compliance_pct !== undefined && p.compliance_pct !== null
                        ? `${p.compliance_pct.toFixed(0)}%`
                        : '—'}
                    </span>
                    <span className="text-slate-500">compliant</span>
                  </div>
                  <div className="text-slate-500">
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
