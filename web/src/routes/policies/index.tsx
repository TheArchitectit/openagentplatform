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
      return 'bg-danger/10 text-danger border-danger/20';
    case 'compliance':
      return 'bg-info/10 text-info border-info/20';
    case 'configuration':
      return 'bg-accent/10 text-accent border-accent/20';
    case 'performance':
      return 'bg-warning/10 text-warning border-warning/20';
    case 'custom':
    default:
      return 'bg-text-muted/10 text-text-secondary border-text-muted/20';
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
      return 'bg-danger/10 text-danger border-danger/20';
    case 'audit':
      return 'bg-warning/10 text-warning border-warning/20';
    case 'report':
    default:
      return 'bg-info/10 text-info border-info/20';
  }
}

function complianceColor(pct: number | undefined): string {
  if (pct === undefined || pct === null) return 'text-text-muted';
  if (pct >= 80) return 'text-success';
  if (pct >= 60) return 'text-warning';
  return 'text-danger';
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
          <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center" aria-hidden="true">
            <ShieldCheck className="h-4 w-4 text-text-secondary" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">Policy Library</h1>
            <p className="text-text-secondary text-sm mt-0.5">
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
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent-hover text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
          className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle overflow-x-auto"
        >
          {CATEGORY_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={category === opt.value}
              onClick={() => setCategory(opt.value)}
              className={
                'px-3 h-8 rounded text-sm transition-colors whitespace-nowrap focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ' +
                (category === opt.value
                  ? 'bg-surface-tertiary text-text-primary'
                  : 'text-text-secondary hover:text-text-primary')
              }
            >
              {opt.label}
              <span className="ml-2 text-xs text-text-muted" aria-hidden="true">{counts[opt.value] ?? 0}</span>
              <span className="sr-only">({counts[opt.value] ?? 0} policies)</span>
            </button>
          ))}
        </div>

        <div className="flex items-center justify-between flex-wrap gap-3">
          <div
            role="group"
            aria-label="Enforcement mode"
            className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle"
          >
            {ENFORCEMENT_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                aria-pressed={enforcement === opt.value}
                onClick={() => setEnforcement(opt.value)}
                className={
                  'px-3 h-8 rounded text-sm transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ' +
                  (enforcement === opt.value
                    ? 'bg-surface-tertiary text-text-primary'
                    : 'text-text-secondary hover:text-text-primary')
                }
              >
                {opt.label}
              </button>
            ))}
          </div>

          <div className="relative w-full sm:w-72" role="search">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
            <input
              type="search"
              role="searchbox"
              aria-label="Search policies"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search policies…"
              className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
            />
          </div>
        </div>
      </div>

      {saveError && (
        <div role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
          {saveError}
        </div>
      )}

      {/* Card grid */}
      {isLoading && policies.length === 0 ? (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted" role="status" aria-live="polite">
          Loading policies…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-danger/30 bg-danger/5 p-12 text-center text-danger" role="alert">
          Failed to load policies: {error.message}
        </div>
      ) : filtered.length === 0 ? (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted" role="status">
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
                className="text-left rounded-lg border border-border-subtle bg-surface-secondary/60 p-5 hover:border-border-strong hover:bg-surface-secondary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
              >
                <div className="flex items-start justify-between mb-3">
                  <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center shrink-0" aria-hidden="true">
                    <ShieldCheck className="h-4 w-4 text-accent" />
                  </div>
                  <div className="flex items-center gap-2">
                    {!p.enabled && (
                      <span className="inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border bg-text-muted/10 text-text-muted border-text-muted/20" role="status" aria-label="Status: disabled">
                        Disabled
                      </span>
                    )}
                  </div>
                </div>

                <h3 className="text-sm font-semibold text-text-primary truncate">{p.name}</h3>
                {p.description && (
                  <p className="text-xs text-text-secondary mt-1 line-clamp-2">{p.description}</p>
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

                <div className="mt-4 pt-3 border-t border-border-subtle flex items-center justify-between text-xs">
                  <div className="flex items-center gap-1.5 text-text-secondary">
                    <ComplIcon className={'h-3.5 w-3.5 ' + complianceColor(p.compliance_pct)} aria-hidden="true" />
                    <span className={complianceColor(p.compliance_pct)}>
                      {p.compliance_pct !== undefined && p.compliance_pct !== null
                        ? `${p.compliance_pct.toFixed(0)}%`
                        : '—'}
                    </span>
                    <span className="text-text-muted">compliant</span>
                  </div>
                  <div className="text-text-muted">
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
