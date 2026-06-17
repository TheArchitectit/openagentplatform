import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import {
  Activity,
  RefreshCw,
  Search,
  Plus,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  X,
  Loader2,
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
} from 'lucide-react';
import { toast } from 'sonner';
import { useChecks, type Check, type CheckStatus, type CheckType } from '@/lib/useChecks';
import { useFocusTrap, useEscapeKey } from '@/lib/a11y';

export const Route = createFileRoute('/checks/')({
  component: ChecksListPage,
});

// ---------------------------------------------------------------------------
// Status / type display
// ---------------------------------------------------------------------------

type Filter = 'all' | 'ok' | 'warning' | 'critical' | 'disabled';

const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

const statusColor: Record<CheckStatus, string> = {
  ok: 'text-success',
  warning: 'text-warning',
  critical: 'text-danger',
  disabled: 'text-text-muted',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-success/10 border-success/30',
  warning: 'bg-warning/10 border-warning/30',
  critical: 'bg-danger/10 border-danger/30',
  disabled: 'bg-text-muted/10 border-text-muted/30',
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
  disk_usage: 'Disk',
  memory_usage: 'Memory',
  cpu_usage: 'CPU',
  process: 'Process',
  service: 'Service',
  tls_cert: 'TLS Cert',
  script: 'Script',
  log_watch: 'Log Watch',
};

function deriveStatus(c: Check): CheckStatus {
  if (!c.enabled) return 'disabled';
  return (c.last_status ?? 'disabled') as CheckStatus;
}

function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

function ChecksListPage() {
  const navigate = useNavigate();
  const { checks, isLoading, error, refresh, status, createCheck, deleteCheck, runCheck } = useChecks();
  const [filter, setFilter] = useState<Filter>('all');
  const [query, setQuery] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [createOpen, setCreateOpen] = useState(false);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return checks.filter((c) => {
      const k = deriveStatus(c);
      if (filter !== 'all' && k !== filter) return false;
      if (q && !c.name.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [checks, filter, query]);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: checks.length, ok: 0, warning: 0, critical: 0, disabled: 0 };
    for (const x of checks) {
      const k = deriveStatus(x);
      c[k] = (c[k] ?? 0) + 1;
    }
    return c;
  }, [checks]);

  const onDelete = async (c: Check) => {
    if (!confirm(`Delete check "${c.name}"?`)) return;
    try {
      await deleteCheck(c.id);
      toast.success(`Deleted "${c.name}"`);
    } catch (e) {
      toast.error(`Delete failed: ${(e as Error).message}`);
    }
  };

  const onRunNow = async (c: Check) => {
    try {
      await runCheck(c.id);
      toast.success(`Triggered "${c.name}"`);
    } catch (e) {
      toast.error(`Run failed: ${(e as Error).message}`);
    }
  };

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center" aria-hidden="true">
            <Activity className="h-4 w-4 text-text-secondary" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">Checks</h1>
            <p className="text-text-secondary text-sm mt-0.5">Health checks running across your fleet.</p>
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
            aria-label="Refresh checks"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent-hover text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            <span>Create Check</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div
          role="tablist"
          aria-label="Filter checks by status"
          className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle flex-wrap"
        >
          {(['all', 'ok', 'warning', 'critical', 'disabled'] as Filter[]).map((f) => (
            <button
              key={f}
              type="button"
              role="tab"
              aria-selected={filter === f}
              onClick={() => setFilter(f)}
              className={
                'px-3 h-8 rounded text-sm capitalize transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ' +
                (filter === f
                  ? 'bg-surface-tertiary text-text-primary'
                  : 'text-text-secondary hover:text-text-primary')
              }
            >
              {f}
              <span className="ml-2 text-xs text-text-muted" aria-hidden="true">{counts[f]}</span>
              <span className="sr-only">({counts[f]} checks)</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search check name"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search check name…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Health checks" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                <th className="px-4 py-3 w-10" scope="col">Status</th>
                <th className="px-4 py-3" scope="col">Name</th>
                <th className="px-4 py-3" scope="col">Type</th>
                <th className="px-4 py-3 text-right" scope="col">Agents</th>
                <th className="px-4 py-3" scope="col">Last Run</th>
                <th className="px-4 py-3" scope="col">Interval</th>
                <th className="px-4 py-3 text-right" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading && checks.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-text-muted" role="status" aria-live="polite">
                    <div className="inline-flex items-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                      <span>Loading checks…</span>
                    </div>
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-danger" role="alert">
                    Failed to load checks: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-text-muted" role="status">
                    No checks match the current filter.
                  </td>
                </tr>
              ) : (
                filtered.map((c) => {
                  const k = deriveStatus(c);
                  const Icon = statusIcon[k];
                  const TypeIcon = typeIcon[c.type] ?? Activity;
                  return (
                    <tr
                      key={c.id}
                      onClick={() => {
                        void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
                      }}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
                        }
                      }}
                      tabIndex={0}
                      className="hover:bg-surface-tertiary/40 cursor-pointer transition-colors focus:outline-none focus-visible:bg-surface-tertiary/60"
                    >
                      <td className="px-4 py-3">
                        <Icon className={'h-4 w-4 ' + statusColor[k]} aria-label={`Status: ${k}`} />
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-text-primary font-medium">{c.name}</span>
                      </td>
                      <td className="px-4 py-3">
                        <span className={'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md border text-xs ' + statusBg[k]}>
                          <TypeIcon className="h-3 w-3" aria-hidden="true" />
                          {typeLabel[c.type] ?? c.type}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-text-primary">
                        {c.assigned_agents ?? 0}
                      </td>
                      <td className="px-4 py-3 text-text-secondary">{formatTime(c.last_run, now)}</td>
                      <td className="px-4 py-3 text-text-secondary">{formatInterval(c.interval_secs)}</td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1" role="group" aria-label={`Actions for check ${c.name}`}>
                          <button
                            type="button"
                            onClick={() => {
                              void onRunNow(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-text-secondary hover:bg-border-strong border border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                            aria-label={`Run check ${c.name} now`}
                          >
                            Run
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              void onDelete(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-danger hover:bg-danger/10 border border-danger/30 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                            aria-label={`Delete check ${c.name}`}
                          >
                            Delete
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

      {createOpen && (
        <CreateCheckModal
          onClose={() => setCreateOpen(false)}
          onSubmit={async (input) => {
            try {
              const c = await createCheck(input);
              toast.success(`Created "${c.name}"`);
              setCreateOpen(false);
              void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
            } catch (e) {
              toast.error(`Create failed: ${(e as Error).message}`);
            }
          }}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Create modal
// ---------------------------------------------------------------------------

interface CheckTypeDef {
  label: string;
  icon: typeof Globe;
  defaults: Record<string, unknown>;
  fields: ConfigField[];
}

interface ConfigField {
  key: string;
  label: string;
  type: 'text' | 'number' | 'select';
  placeholder?: string;
  options?: { value: string; label: string }[];
  required?: boolean;
}

const checkTypeDefs: Record<CheckType, CheckTypeDef> = {
  http: {
    label: 'HTTP',
    icon: Globe,
    defaults: { url: '', method: 'GET', expected_status: 200, timeout_secs: 10 },
    fields: [
      { key: 'url', label: 'URL', type: 'text', placeholder: 'https://example.com/health', required: true },
      { key: 'method', label: 'Method', type: 'select', options: [
        { value: 'GET', label: 'GET' },
        { value: 'POST', label: 'POST' },
        { value: 'HEAD', label: 'HEAD' },
      ]},
      { key: 'expected_status', label: 'Expected status code', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  tcp: {
    label: 'TCP',
    icon: Network,
    defaults: { host: '', port: 443, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  ping: {
    label: 'Ping',
    icon: Radio,
    defaults: { host: '', count: 3, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'count', label: 'Packet count', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  disk_usage: {
    label: 'Disk Usage',
    icon: HardDrive,
    defaults: { path: '/', warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'path', label: 'Path', type: 'text', placeholder: '/', required: true },
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  memory_usage: {
    label: 'Memory Usage',
    icon: MemoryStick,
    defaults: { warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  cpu_usage: {
    label: 'CPU Usage',
    icon: Cpu,
    defaults: { warn_pct: 80, crit_pct: 95, window_secs: 30 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
      { key: 'window_secs', label: 'Sample window (s)', type: 'number' },
    ],
  },
  process: {
    label: 'Process',
    icon: ServerCog,
    defaults: { name: '', expected: 'running' },
    fields: [
      { key: 'name', label: 'Process name', type: 'text', placeholder: 'nginx', required: true },
      { key: 'expected', label: 'Expected state', type: 'select', options: [
        { value: 'running', label: 'Running' },
        { value: 'stopped', label: 'Stopped' },
      ]},
    ],
  },
  service: {
    label: 'Service',
    icon: ServerCog,
    defaults: { name: '' },
    fields: [
      { key: 'name', label: 'Service name', type: 'text', placeholder: 'nginx.service', required: true },
    ],
  },
  tls_cert: {
    label: 'TLS Certificate',
    icon: ShieldCheck,
    defaults: { host: '', port: 443, warn_days: 30, crit_days: 7 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number' },
      { key: 'warn_days', label: 'Warn (days remaining)', type: 'number' },
      { key: 'crit_days', label: 'Critical (days remaining)', type: 'number' },
    ],
  },
  script: {
    label: 'Script',
    icon: FileCode2,
    defaults: { script_id: '', timeout_secs: 30 },
    fields: [
      { key: 'script_id', label: 'Script ID', type: 'text', placeholder: 'script-uuid', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  log_watch: {
    label: 'Log Watch',
    icon: ScrollText,
    defaults: { path: '', pattern: '' },
    fields: [
      { key: 'path', label: 'Log path', type: 'text', placeholder: '/var/log/syslog', required: true },
      { key: 'pattern', label: 'Regex pattern', type: 'text', required: true },
    ],
  },
};

const allCheckTypes: CheckType[] = [
  'http', 'tcp', 'ping', 'disk_usage', 'memory_usage', 'cpu_usage',
  'process', 'service', 'tls_cert', 'script', 'log_watch',
];

interface CreateCheckModalProps {
  onClose: () => void;
  onSubmit: (input: { name: string; type: CheckType; config: Record<string, unknown>; interval_secs: number }) => Promise<void>;
}

function CreateCheckModal({ onClose, onSubmit }: CreateCheckModalProps) {
  const [name, setName] = useState('');
  const [type, setType] = useState<CheckType>('http');
  const [interval, setInterval] = useState(60);
  const [config, setConfig] = useState<Record<string, unknown>>({ ...checkTypeDefs.http.defaults });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const baseId = useId();
  const nameId = `${baseId}-name`;
  const typeId = `${baseId}-type`;
  const intervalId = `${baseId}-interval`;
  const errorId = `${baseId}-error`;
  const titleId = `${baseId}-title`;
  const dialogRef = useRef<HTMLDivElement>(null);

  // Trap focus and handle Escape.  useFocusTrap restores focus on unmount.
  useFocusTrap(dialogRef);
  useEscapeKey(onClose);

  const onChangeType = (next: CheckType) => {
    setType(next);
    setConfig({ ...checkTypeDefs[next].defaults });
  };

  const setField = (key: string, value: unknown) => {
    setConfig((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (interval < 10) {
      setError('Interval must be at least 10 seconds');
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit({ name: name.trim(), type, config, interval_secs: interval });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const def = checkTypeDefs[type];

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={error ? errorId : undefined}
        className="w-full max-w-lg rounded-lg border border-border-subtle bg-surface-secondary shadow-xl"
      >
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 id={titleId} className="text-sm font-semibold text-text-primary">Create Check</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4" noValidate>
          <div>
            <label htmlFor={nameId} className="block text-xs text-text-secondary mb-1">
              Name <span aria-hidden="true" className="text-danger">*</span>
            </label>
            <input
              id={nameId}
              type="text"
              required
              aria-required="true"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Disk usage on prod"
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
            />
          </div>

          <div>
            <label htmlFor={typeId} className="block text-xs text-text-secondary mb-1">Type</label>
            <select
              id={typeId}
              value={type}
              onChange={(e) => onChangeType(e.target.value as CheckType)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
            >
              {allCheckTypes.map((t) => (
                <option key={t} value={t}>
                  {checkTypeDefs[t].label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor={intervalId} className="block text-xs text-text-secondary mb-1">Interval (seconds, min 10)</label>
            <input
              id={intervalId}
              type="number"
              value={interval}
              min={10}
              aria-required="true"
              onChange={(e) => setInterval(Number(e.target.value) || 60)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
            />
          </div>

          <div className="rounded-md border border-border-subtle bg-surface-primary/40 p-3 space-y-3">
            <p className="text-xs text-text-muted uppercase tracking-wider">Config ({def.label})</p>
            {def.fields.map((f) => {
              const fieldId = `${baseId}-field-${f.key}`;
              return (
                <div key={f.key}>
                  <label htmlFor={fieldId} className="block text-xs text-text-secondary mb-1">
                    {f.label}
                    {f.required && <span aria-hidden="true" className="text-danger ml-0.5">*</span>}
                  </label>
                  {f.type === 'select' ? (
                    <select
                      id={fieldId}
                      value={String(config[f.key] ?? '')}
                      required={f.required}
                      aria-required={f.required ? 'true' : undefined}
                      onChange={(e) => setField(f.key, e.target.value)}
                      className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
                    >
                      {f.options?.map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <input
                      id={fieldId}
                      type={f.type}
                      value={String(config[f.key] ?? '')}
                      placeholder={f.placeholder}
                      required={f.required}
                      aria-required={f.required ? 'true' : undefined}
                      onChange={(e) => setField(f.key, f.type === 'number' ? Number(e.target.value) : e.target.value)}
                      className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
                    />
                  )}
                </div>
              );
            })}
          </div>

          {error && (
            <div
              id={errorId}
              role="alert"
              className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger"
            >
              {error}
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-border-strong bg-surface-tertiary text-sm text-text-primary hover:bg-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent-hover text-sm text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
            >
              {submitting && <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />}
              <span>{submitting ? 'Creating…' : 'Create Check'}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
