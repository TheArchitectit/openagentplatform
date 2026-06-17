// Scripts — script library landing page.
//
// Features:
//   • Filter tabs: All, Bash, PowerShell, Python, Node
//   • Search by name/description
//   • Table: Name, Runtime badge, Description, Last Run, Status, Actions
//   • Row click → /scripts/$scriptId
//   • Create Script button → /scripts/new
//   • Delete script action
//   • Run-now action per script (single agent prompt)

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import {
  FileCode2,
  Plus,
  Search,
  RefreshCw,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  CirclePlay,
  Loader2,
  Trash2,
  Terminal,
  Globe,
  Code2,
  Braces,
} from 'lucide-react';
import { toast } from 'sonner';
import { useScripts, type Script, type ScriptRuntime, type ScriptRunStatus } from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';

export const Route = createFileRoute('/scripts/')({
  component: ScriptsListPage,
});

type RuntimeFilter = 'all' | ScriptRuntime;

const RUNTIME_TABS: { id: RuntimeFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'bash', label: 'Bash' },
  { id: 'powershell', label: 'PowerShell' },
  { id: 'python', label: 'Python' },
  { id: 'node', label: 'Node' },
];

const RUNTIME_META: Record<ScriptRuntime, { label: string; icon: typeof Terminal; classes: string }> = {
  bash: {
    label: 'Bash',
    icon: Terminal,
    classes: 'bg-success/10 text-success border-success/20',
  },
  powershell: {
    label: 'PowerShell',
    icon: Terminal,
    classes: 'bg-info/10 text-info border-info/20',
  },
  python: {
    label: 'Python',
    icon: Code2,
    classes: 'bg-warning/10 text-warning border-warning/20',
  },
  node: {
    label: 'Node',
    icon: Braces,
    classes: 'bg-accent/10 text-accent border-accent/20',
  },
};

const STATUS_META: Record<
  ScriptRunStatus,
  { label: string; classes: string; icon: typeof CircleCheck }
> = {
  pending: {
    label: 'Pending',
    classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20',
    icon: CircleDashed,
  },
  in_progress: {
    label: 'Running',
    classes: 'bg-accent/10 text-accent border-accent/20',
    icon: CirclePlay,
  },
  completed: {
    label: 'Success',
    classes: 'bg-success/10 text-success border-success/20',
    icon: CircleCheck,
  },
  failed: {
    label: 'Failed',
    classes: 'bg-danger/10 text-danger border-danger/20',
    icon: CircleX,
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20',
    icon: CircleDashed,
  },
  timeout: {
    label: 'Timeout',
    classes: 'bg-warning/10 text-warning border-warning/20',
    icon: CircleAlert,
  },
};

function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

function ScriptsListPage() {
  const navigate = useNavigate();
  const { scripts, isLoading, error, refresh, status, deleteScript, runScript } = useScripts();
  const { agents } = useAgents();
  const [filter, setFilter] = useState<RuntimeFilter>('all');
  const [query, setQuery] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [busyId, setBusyId] = useState<string | null>(null);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return scripts.filter((s) => {
      if (filter !== 'all' && s.runtime !== filter) return false;
      if (!q) return true;
      if (s.name.toLowerCase().includes(q)) return true;
      if (s.description?.toLowerCase().includes(q)) return true;
      if (s.tags?.some((t) => t.toLowerCase().includes(q))) return true;
      return false;
    });
  }, [scripts, filter, query]);

  const counts = useMemo(() => {
    const c: Record<RuntimeFilter, number> = {
      all: scripts.length,
      bash: 0,
      powershell: 0,
      python: 0,
      node: 0,
    };
    for (const s of scripts) {
      c[s.runtime] = (c[s.runtime] ?? 0) + 1;
    }
    return c;
  }, [scripts]);

  const onDelete = async (s: Script) => {
    if (!confirm(`Delete script "${s.name}"? This cannot be undone.`)) return;
    setBusyId(s.id);
    try {
      await deleteScript(s.id);
      toast.success(`Deleted "${s.name}"`);
    } catch (e) {
      toast.error(`Delete failed: ${(e as Error).message}`);
    } finally {
      setBusyId(null);
    }
  };

  const onRunNow = async (s: Script) => {
    // Pick the first online agent as default; if none online, pick the first
    // agent. In a more advanced UI, we'd show a picker modal.
    const candidate = agents.find((a) => a.status === 'online') ?? agents[0];
    if (!candidate) {
      toast.error('No agents available to run this script');
      return;
    }
    setBusyId(s.id);
    try {
      await runScript(s.id, [candidate.id]);
      toast.success(`Run started on ${candidate.hostname}`);
    } catch (e) {
      toast.error(`Run failed: ${(e as Error).message}`);
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-accent/10 border border-accent/20 flex items-center justify-center" aria-hidden="true">
            <FileCode2 className="h-4 w-4 text-accent" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">Scripts</h1>
            <p className="text-text-secondary text-sm mt-0.5">
              Reusable script library for on-demand execution across the fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-success' : status === 'connecting' ? 'bg-warning' : 'bg-text-muted')
            }
            role="status"
            aria-label={`WebSocket connection: ${status}`}
          />
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            aria-label="Refresh scripts"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => {
              void navigate({ to: '/scripts/new' });
            }}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent-hover text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            <span>Create Script</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div
          role="tablist"
          aria-label="Filter scripts by runtime"
          className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle flex-wrap"
        >
          {RUNTIME_TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={filter === t.id}
              onClick={() => setFilter(t.id)}
              className={
                'px-3 h-8 rounded text-sm transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ' +
                (filter === t.id
                  ? 'bg-surface-tertiary text-text-primary'
                  : 'text-text-secondary hover:text-text-primary')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-text-muted" aria-hidden="true">{counts[t.id]}</span>
              <span className="sr-only">({counts[t.id]} scripts)</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search scripts"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search scripts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Scripts" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                <th className="px-4 py-3" scope="col">Name</th>
                <th className="px-4 py-3 w-32" scope="col">Runtime</th>
                <th className="px-4 py-3" scope="col">Description</th>
                <th className="px-4 py-3 w-32" scope="col">Last Run</th>
                <th className="px-4 py-3 w-28" scope="col">Status</th>
                <th className="px-4 py-3 text-right w-32" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading && scripts.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-text-muted" role="status" aria-live="polite">
                    <div className="inline-flex items-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                      <span>Loading scripts…</span>
                    </div>
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-danger" role="alert">
                    Failed to load scripts: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-text-muted" role="status">
                    No scripts match the current filter.
                  </td>
                </tr>
              ) : (
                filtered.map((s) => {
                  const meta = s.last_status ? STATUS_META[s.last_status] : null;
                  const runtimeMeta = RUNTIME_META[s.runtime];
                  const RuntimeIcon = runtimeMeta.icon;
                  const isBusy = busyId === s.id;
                  return (
                    <tr
                      key={s.id}
                      onClick={() => {
                        void navigate({ to: '/scripts/$scriptId', params: { scriptId: s.id } });
                      }}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          void navigate({ to: '/scripts/$scriptId', params: { scriptId: s.id } });
                        }
                      }}
                      tabIndex={0}
                      aria-label={`Script: ${s.name}. Press Enter to view details.`}
                      className="hover:bg-surface-tertiary/40 cursor-pointer transition-colors focus:outline-none focus-visible:bg-surface-tertiary/60"
                    >
                      <td className="px-4 py-3">
                        <div className="flex flex-col">
                          <span className="text-text-primary font-medium">{s.name}</span>
                          {s.tags && s.tags.length > 0 && (
                            <div className="flex flex-wrap gap-1 mt-1">
                              {s.tags.slice(0, 3).map((tag) => (
                                <span
                                  key={tag}
                                  className="inline-flex px-1.5 py-0.5 rounded text-[10px] bg-surface-tertiary border border-border-strong text-text-secondary"
                                >
                                  {tag}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={
                            'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md border text-xs ' +
                            runtimeMeta.classes
                          }
                        >
                          <RuntimeIcon className="h-3 w-3" />
                          {runtimeMeta.label}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-text-secondary max-w-xs truncate">
                        {s.description ?? '—'}
                      </td>
                      <td className="px-4 py-3 text-text-secondary">
                        {formatRelative(s.last_run, now)}
                      </td>
                      <td className="px-4 py-3">
                        {meta ? (
                          <span
                            className={
                              'inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-xs ' +
                              meta.classes
                            }
                            role="status"
                            aria-label={`Status: ${meta.label}`}
                          >
                            <meta.icon className="h-3 w-3" aria-hidden="true" />
                            {meta.label}
                          </span>
                        ) : (
                          <span className="text-xs text-text-muted">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1" role="group" aria-label={`Actions for script ${s.name}`}>
                          <button
                            type="button"
                            disabled={isBusy}
                            onClick={() => {
                              void onRunNow(s);
                            }}
                            className="inline-flex items-center gap-1 px-2 h-7 rounded text-xs text-text-secondary hover:bg-border-strong border border-border-strong disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
                            aria-label={`Run script ${s.name} now`}
                          >
                            {isBusy ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                            ) : (
                              <Globe className="h-3.5 w-3.5" aria-hidden="true" />
                            )}
                            <span>Run</span>
                          </button>
                          <button
                            type="button"
                            disabled={isBusy}
                            onClick={() => {
                              void onDelete(s);
                            }}
                            className="inline-flex items-center gap-1 px-2 h-7 rounded text-xs text-danger hover:bg-danger/10 border border-danger/30 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
                            aria-label={`Delete script ${s.name}`}
                          >
                            <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
