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
// Create modal
// ---------------------------------------------------------------------------

export interface CheckTypeDef {
  label: string;
  icon: typeof Globe;
  defaults: Record<string, unknown>;
  fields: ConfigField[];
}

export interface ConfigField {
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

export interface CreateCheckModalProps {
  onClose: () => void;
  onSubmit: (input: { name: string; type: CheckType; config: Record<string, unknown>; interval_secs: number }) => Promise<void>;
}

export function CreateCheckModal({ onClose, onSubmit }: CreateCheckModalProps) {
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
        className="w-full max-w-lg rounded-lg border border-slate-800 bg-slate-900 shadow-xl"
      >
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 id={titleId} className="text-sm font-semibold text-white">Create Check</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          >
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4" noValidate>
          <div>
            <label htmlFor={nameId} className="block text-xs text-gray-300 mb-1">
              Name <span aria-hidden="true" className="text-red-400">*</span>
            </label>
            <input
              id={nameId}
              type="text"
              required
              aria-required="true"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Disk usage on prod"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>

          <div>
            <label htmlFor={typeId} className="block text-xs text-gray-300 mb-1">Type</label>
            <select
              id={typeId}
              value={type}
              onChange={(e) => onChangeType(e.target.value as CheckType)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            >
              {allCheckTypes.map((t) => (
                <option key={t} value={t}>
                  {checkTypeDefs[t].label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor={intervalId} className="block text-xs text-gray-300 mb-1">Interval (seconds, min 10)</label>
            <input
              id={intervalId}
              type="number"
              value={interval}
              min={10}
              aria-required="true"
              onChange={(e) => setInterval(Number(e.target.value) || 60)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>

          <div className="rounded-md border border-slate-800 bg-slate-800 p-3 space-y-3">
            <p className="text-xs text-gray-400 uppercase tracking-wider">Config ({def.label})</p>
            {def.fields.map((f) => {
              const fieldId = `${baseId}-field-${f.key}`;
              return (
                <div key={f.key}>
                  <label htmlFor={fieldId} className="block text-xs text-gray-300 mb-1">
                    {f.label}
                    {f.required && <span aria-hidden="true" className="text-red-400 ml-0.5">*</span>}
                  </label>
                  {f.type === 'select' ? (
                    <select
                      id={fieldId}
                      value={String(config[f.key] ?? '')}
                      required={f.required}
                      aria-required={f.required ? 'true' : undefined}
                      onChange={(e) => setField(f.key, e.target.value)}
                      className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
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
                      className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
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
              className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400"
            >
              {error}
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-white hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
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

function ChecksListPage() {
  return <div>Checks List</div>;
}
