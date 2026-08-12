import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft,
  Activity,
  Play,
  Trash2,
  Plus,
  Bot,
  Loader2,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  Globe,
  Network,
  HardDrive,
  Cpu,
  MemoryStick,
  ServerCog,
  FileCode2,
  ScrollText,
  ShieldCheck,
  Radio,
  Save,
  Power,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { useAgents, type Agent } from '@/lib/useAgents';
import {
  useChecks,
  type Check,
  type CheckResult,
  type CheckStatus,
  type CheckType,
  type AgentAssignment,
} from '@/lib/useChecks';
import { MonacoEditor } from '@/components/monaco-editor';

export const Route = createFileRoute('/checks/$checkId')({
  component: CheckDetailPage,
});

// ---------------------------------------------------------------------------
// Display helpers (mirrored from the list page; kept local for self-containment)
// ---------------------------------------------------------------------------

const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

const statusColor: Record<CheckStatus, string> = {
  ok: 'text-green-400',
  warning: 'text-yellow-400',
  critical: 'text-red-400',
  disabled: 'text-gray-400',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-green-500/10 text-green-400 border-green-800',
  warning: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  critical: 'bg-red-500/10 text-red-400 border-red-800',
  disabled: 'bg-slate-500/10 text-gray-300 border-slate-700',
};

const typeIcon: Record<CheckType, typeof Globe> = {
  http: Globe,
  tcp: Network,
  ping: Radio,
  disk_usage: HardDrive,
  memory_usage: MemoryStick,
  cpu_usage: Cpu,
  process: ServerCog,
  service: ServerCog,
  tls_cert: ShieldCheck,
  script: FileCode2,
  log_watch: ScrollText,
};

const typeLabel: Record<CheckType, string> = {
  http: 'HTTP',
  tcp: 'TCP',
  ping: 'Ping',
  disk_usage: 'Disk Usage',
  memory_usage: 'Memory Usage',
  cpu_usage: 'CPU Usage',
  process: 'Process',
  service: 'Service',
  tls_cert: 'TLS Certificate',
  script: 'Script',
  log_watch: 'Log Watch',
};


export function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

export function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

export function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

export function deriveStatus(c: Check | null | undefined): CheckStatus {
  if (!c) return 'disabled';
  if (!c.enabled) return 'disabled';
  return (c.last_status ?? 'disabled') as CheckStatus;
}


export function ResultBarChart({ results }: { results: CheckResult[] }) {
  // Bucket results by time (oldest -> newest). Up to 20 bars.
  const bars = useMemo(() => {
    if (results.length === 0) return [] as { status: CheckStatus; label: string }[];
    const sorted = [...results]
      .filter((r) => !!r.timestamp)
      .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
    return sorted.map((r) => {
      const ts = new Date(r.timestamp);
      const hh = ts.getHours().toString().padStart(2, '0');
      const mm = ts.getMinutes().toString().padStart(2, '0');
      return {
        status: (r.status as CheckStatus) ?? 'disabled',
        label: `${hh}:${mm}`,
      };
    });
  }, [results]);

  if (bars.length === 0) {
    return (
      <div className="text-center text-gray-400 text-sm py-8">No results to chart yet.</div>
    );
  }

  return (
    <div className="flex items-end gap-1 h-32">
      {bars.map((b, i) => {
        const color =
          b.status === 'ok'
            ? 'bg-green-500'
            : b.status === 'warning'
            ? 'bg-yellow-500'
            : b.status === 'critical'
            ? 'bg-red-500'
            : 'bg-slate-700';
        // Show timestamp labels only on first, middle, and last to avoid clutter.
        const showLabel = bars.length <= 6 || i === 0 || i === Math.floor(bars.length / 2) || i === bars.length - 1;
        return (
          <div key={i} className="flex-1 flex flex-col items-center justify-end gap-1 min-w-0">
            <div
              className={'w-full rounded-t ' + color}
              style={{ height: b.status === 'disabled' ? '4px' : '100%' }}
              title={b.status}
            />
            {showLabel && (
              <span className="text-[10px] text-gray-400 truncate w-full text-center">{b.label}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Assign Agent modal
// ---------------------------------------------------------------------------

export interface AssignAgentModalProps {
  agents: Agent[];
  assignedIds: Set<string>;
  onClose: () => void;
  onAssign: (agentId: string) => Promise<void>;
}

export function AssignAgentModal({ agents, assignedIds, onClose, onAssign }: AssignAgentModalProps) {
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState(false);

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return agents
      .filter((a) => !assignedIds.has(a.id))
      .filter((a) => !q || a.hostname.toLowerCase().includes(q))
      .slice(0, 50);
  }, [agents, assignedIds, query]);

  const handleAssign = async (id: string) => {
    setBusy(true);
    try {
      await onAssign(id);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md rounded-lg border border-slate-800 bg-slate-900 shadow-xl">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white">Assign Agent</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-gray-300 hover:text-white hover:bg-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-4">
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search agents…"
            className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40 mb-3"
          />
          <ul className="max-h-80 overflow-y-auto divide-y divide-slate-800 rounded-md border border-slate-800">
            {candidates.length === 0 ? (
              <li className="px-3 py-6 text-center text-gray-400 text-sm">No agents available.</li>
            ) : (
              candidates.map((a) => (
                <li key={a.id} className="px-3 py-2 flex items-center justify-between hover:bg-slate-800/40">
                  <div className="flex items-center gap-2 min-w-0">
                    <Bot className="h-4 w-4 text-gray-400 shrink-0" />
                    <span className="text-sm text-white truncate">{a.hostname || a.id}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      void handleAssign(a.id);
                    }}
                    disabled={busy}
                    className="px-2 h-7 rounded text-xs text-blue-400 hover:bg-blue-600/10 border border-blue-500/30 disabled:opacity-50"
                  >
                    Assign
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Edit modal
// ---------------------------------------------------------------------------

export interface EditCheckModalProps {
  check: Check;
  onClose: () => void;
  onSubmit: (patch: { name?: string; interval_secs?: number; config?: Record<string, unknown> }) => Promise<void>;
}

export function EditCheckModal({ check, onClose, onSubmit }: EditCheckModalProps) {
  const [name, setName] = useState(check.name);
  const [interval, setInterval] = useState(check.interval_secs);
  const [configJson, setConfigJson] = useState(JSON.stringify(check.config ?? {}, null, 2));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    let config: Record<string, unknown>;
    try {
      const parsed = JSON.parse(configJson);
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        throw new Error('Config must be a JSON object');
      }
      config = parsed as Record<string, unknown>;
    } catch (e) {
      setError(`Invalid config JSON: ${(e as Error).message}`);
      return;
    }
    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (interval < 10) {
      setError('Interval must be at least 10 seconds');
      return;
    }
    setBusy(true);
    try {
      await onSubmit({ name: name.trim(), interval_secs: interval, config });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-lg rounded-lg border border-slate-800 bg-slate-900 shadow-xl">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white">Edit Check</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-gray-300 hover:text-white hover:bg-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div>
            <label className="block text-xs text-gray-300 mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
          </div>
          <div>
            <label className="block text-xs text-gray-300 mb-1">Interval (seconds, min 10)</label>
            <input
              type="number"
              value={interval}
              min={10}
              onChange={(e) => setInterval(Number(e.target.value) || 60)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
          </div>
          <div>
            <label className="block text-xs text-gray-300 mb-1">Config (JSON)</label>
            <MonacoEditor
              value={configJson}
              onChange={(v) => setConfigJson(v)}
              language="json"
              height={220}
              theme="vs-dark"
              options={{
                fontSize: 12,
                minimap: { enabled: false },
                lineNumbers: 'on',
                tabSize: 2,
                formatOnPaste: true,
              }}
            />
          </div>
          {error && (
            <div className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
              {error}
            </div>
          )}
          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-white hover:bg-slate-700 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 text-sm text-white disabled:opacity-50 transition-colors"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{busy ? 'Saving…' : 'Save'}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function CheckDetailPage() {
  return <div>Check Detail</div>;
}
