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
    classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20',
  },
  powershell: {
    label: 'PowerShell',
    icon: Terminal,
    classes: 'bg-sky-500/10 text-sky-300 border-sky-500/20',
  },
  python: {
    label: 'Python',
    icon: Code2,
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
  },
  node: {
    label: 'Node',
    icon: Braces,
    classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20',
  },
};

const STATUS_META: Record<
  ScriptRunStatus,
  { label: string; classes: string; icon: typeof CircleCheck }
> = {
  pending: {
    label: 'Pending',
    classes: 'bg-slate-500/10 text-slate-300 border-slate-500/20',
    icon: CircleDashed,
  },
  in_progress: {
    label: 'Running',
    classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20',
    icon: CirclePlay,
  },
  completed: {
    label: 'Success',
    classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20',
    icon: CircleCheck,
  },
  failed: {
    label: 'Failed',
    classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20',
    icon: CircleX,
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-slate-500/10 text-slate-400 border-slate-500/20',
    icon: CircleDashed,
  },
  timeout: {
    label: 'Timeout',
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
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
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center">
            <FileCode2 className="h-4 w-4 text-indigo-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Scripts</h1>
            <p className="text-slate-400 text-sm mt-0.5">
              Reusable script library for on-demand execution across the fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-emerald-500' : status === 'connecting' ? 'bg-amber-500' : 'bg-slate-500')
            }
            title={`WebSocket: ${status}`}
          />
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
              void navigate({ to: '/scripts/new' });
            }}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white transition-colors"
          >
            <Plus className="h-4 w-4" />
            <span>Create Script</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 flex-wrap">
          {RUNTIME_TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setFilter(t.id)}
              className={
                'px-3 h-8 rounded text-sm transition-colors ' +
                (filter === t.id
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:text-slate-200')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-slate-500">{counts[t.id]}</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search scripts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3 w-32">Runtime</th>
                <th className="px-4 py-3">Description</th>
                <th className="px-4 py-3 w-32">Last Run</th>
                <th className="px-4 py-3 w-28">Status</th>
                <th className="px-4 py-3 text-right w-32">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && scripts.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-slate-500">
                    <div className="inline-flex items-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>Loading scripts…</span>
                    </div>
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-rose-400">
                    Failed to load scripts: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-slate-500">
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
                      className="hover:bg-slate-800/40 cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3">
                        <div className="flex flex-col">
                          <span className="text-slate-100 font-medium">{s.name}</span>
                          {s.tags && s.tags.length > 0 && (
                            <div className="flex flex-wrap gap-1 mt-1">
                              {s.tags.slice(0, 3).map((tag) => (
                                <span
                                  key={tag}
                                  className="inline-flex px-1.5 py-0.5 rounded text-[10px] bg-slate-800 border border-slate-700 text-slate-400"
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
                      <td className="px-4 py-3 text-slate-400 max-w-xs truncate">
                        {s.description ?? '—'}
                      </td>
                      <td className="px-4 py-3 text-slate-400">
                        {formatRelative(s.last_run, now)}
                      </td>
                      <td className="px-4 py-3">
                        {meta ? (
                          <span
                            className={
                              'inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-xs ' +
                              meta.classes
                            }
                          >
                            <meta.icon className="h-3 w-3" />
                            {meta.label}
                          </span>
                        ) : (
                          <span className="text-xs text-slate-500">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1">
                          <button
                            type="button"
                            disabled={isBusy}
                            onClick={() => {
                              void onRunNow(s);
                            }}
                            className="inline-flex items-center gap-1 px-2 h-7 rounded text-xs text-slate-300 hover:bg-slate-700 border border-slate-700 disabled:opacity-50 transition-colors"
                            title="Run now"
                          >
                            {isBusy ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <Globe className="h-3.5 w-3.5" />
                            )}
                            <span>Run</span>
                          </button>
                          <button
                            type="button"
                            disabled={isBusy}
                            onClick={() => {
                              void onDelete(s);
                            }}
                            className="inline-flex items-center gap-1 px-2 h-7 rounded text-xs text-rose-300 hover:bg-rose-500/10 border border-rose-500/30 disabled:opacity-50 transition-colors"
                            title="Delete"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
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
