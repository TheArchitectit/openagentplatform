// A2A Agent Detail — shows the full agent card, skill catalog, model
// pricing, health telemetry, and a test-invoke panel for ad-hoc calls.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import {
  ArrowLeft,
  Cpu,
  ExternalLink,
  Send,
  Server,
  Activity,
  MemoryStick,
  Clock,
  Layers,
  Loader2,
  CheckCircle2,
  XCircle,
  AlertCircle,
} from 'lucide-react';
import {
  fetchAdapterCard,
  fetchAdapterHealth,
  invokeAdapter,
  type A2AAdapter,
  type A2AInvokeResult,
} from '@/lib/useA2A';
import { ApiError } from '@/lib/api';

export const Route = createFileRoute('/a2a/agents/$name')({
  component: AgentDetailPage,
});

interface HealthInfo {
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  uptime_secs: number;
  active_tasks: number;
  memory_mb: number;
}

function formatUptime(secs: number): string {
  if (!secs || secs < 0) return '—';
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function statusBadgeClasses(status: HealthInfo['status']): string {
  switch (status) {
    case 'healthy':
      return 'bg-success/10 text-success border-success/20';
    case 'degraded':
      return 'bg-warning/10 text-warning border-warning/20';
    case 'unhealthy':
      return 'bg-danger/10 text-danger border-danger/20';
    default:
      return 'bg-text-muted/10 text-text-secondary border-text-muted/20';
  }
}

function statusIcon(status: HealthInfo['status']) {
  switch (status) {
    case 'healthy':
      return <CheckCircle2 className="h-3.5 w-3.5" />;
    case 'unhealthy':
      return <XCircle className="h-3.5 w-3.5" />;
    case 'degraded':
      return <AlertCircle className="h-3.5 w-3.5" />;
    default:
      return <AlertCircle className="h-3.5 w-3.5" />;
  }
}

function AgentDetailPage() {
  const { name } = Route.useParams();
  const [card, setCard] = useState<A2AAdapter | null>(null);
  const [health, setHealth] = useState<HealthInfo | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Test-invoke panel state
  const [invokeInput, setInvokeInput] = useState('');
  const [invokeResult, setInvokeResult] = useState<A2AInvokeResult | null>(null);
  const [isInvoking, setIsInvoking] = useState(false);
  const [invokeError, setInvokeError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);
    void (async () => {
      try {
        const [c, h] = await Promise.all([
          fetchAdapterCard(name),
          fetchAdapterHealth(name),
        ]);
        if (cancelled) return;
        setCard(c);
        setHealth(h);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Failed to load adapter');
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [name]);

  const handleInvoke = async () => {
    if (!invokeInput.trim() || isInvoking) return;
    setIsInvoking(true);
    setInvokeError(null);
    setInvokeResult(null);
    try {
      const res = await invokeAdapter({ adapter: name, message: invokeInput });
      setInvokeResult(res);
    } catch (err) {
      setInvokeError(err instanceof Error ? err.message : 'Invoke failed');
    } finally {
      setIsInvoking(false);
    }
  };

  if (isLoading) {
    return (
      <div className="text-center py-12 text-text-secondary text-sm">Loading adapter...</div>
    );
  }
  if (error || !card) {
    return (
      <div className="space-y-3">
        <Link
          to="/a2a"
          className="inline-flex items-center gap-1 text-sm text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" /> Back to dashboard
        </Link>
        <div className="p-3 rounded-md border border-danger/30 bg-danger/10 text-danger text-sm">
          {error ?? 'Adapter not found'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Link
          to="/a2a"
          className="p-1.5 rounded-md hover:bg-surface-tertiary text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <div className="flex-1">
          <h1 className="text-2xl font-semibold text-text-primary">{card.display_name ?? card.name}</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            {card.name} · v{card.version}
            {card.provider && ` · ${card.provider}`}
          </p>
        </div>
        {card.url && (
          <a
            href={card.url}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-1 text-xs text-text-secondary hover:text-accent"
          >
            <ExternalLink className="h-3.5 w-3.5" /> Endpoint
          </a>
        )}
      </div>

      {/* Agent card summary */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-2">
          Agent Card
        </h2>
        {card.description && <p className="text-sm text-text-secondary mb-3">{card.description}</p>}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
          <Field label="Name" value={card.name} />
          <Field label="Version" value={card.version} />
          <Field label="Provider" value={card.provider ?? '—'} />
          <Field label="URL" value={card.url ?? '—'} />
        </div>
      </div>

      {/* Health */}
      {health && (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
          <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
            <Activity className="h-4 w-4" /> Health
          </h2>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
            <div>
              <div className="text-xs text-text-muted mb-1">Status</div>
              <span
                className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded border ${statusBadgeClasses(health.status)}`}
              >
                {statusIcon(health.status)}
                {health.status}
              </span>
            </div>
            <Field icon={<Clock className="h-3.5 w-3.5" />} label="Uptime" value={formatUptime(health.uptime_secs)} />
            <Field icon={<Server className="h-3.5 w-3.5" />} label="Active tasks" value={String(health.active_tasks)} />
            <Field icon={<MemoryStick className="h-3.5 w-3.5" />} label="Memory" value={`${health.memory_mb} MB`} />
          </div>
        </div>
      )}

      {/* Skills */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <Layers className="h-4 w-4" /> Skills
        </h2>
        {card.skills && card.skills.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase text-text-muted border-b border-border-subtle">
                  <th className="py-2 pr-3">Skill</th>
                  <th className="py-2 pr-3">Description</th>
                  <th className="py-2 pr-3">Tags</th>
                  <th className="py-2">Schemas</th>
                </tr>
              </thead>
              <tbody>
                {card.skills.map((s) => (
                  <tr key={s.name} className="border-b border-border-subtle/50">
                    <td className="py-2 pr-3 font-mono text-accent">{s.name}</td>
                    <td className="py-2 pr-3 text-text-secondary max-w-xs truncate">{s.description}</td>
                    <td className="py-2 pr-3">
                      <div className="flex flex-wrap gap-1">
                        {s.tags.map((t) => (
                          <span
                            key={t}
                            className="text-[10px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-secondary"
                          >
                            {t}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="py-2 text-xs text-text-muted">
                      {s.input_schema && <span className="mr-2">in</span>}
                      {s.output_schema && <span>out</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-sm text-text-muted">No skills declared.</p>
        )}
      </div>

      {/* Models */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <Cpu className="h-4 w-4" /> Supported Models
        </h2>
        {card.models && card.models.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase text-text-muted border-b border-border-subtle">
                  <th className="py-2 pr-3">Model</th>
                  <th className="py-2 pr-3 text-right">Input / 1K</th>
                  <th className="py-2 text-right">Output / 1K</th>
                </tr>
              </thead>
              <tbody>
                {card.models.map((m) => (
                  <tr key={m.name} className="border-b border-border-subtle/50">
                    <td className="py-2 pr-3 font-mono text-text-primary">{m.name}</td>
                    <td className="py-2 pr-3 text-right text-text-secondary">
                      ${m.input_cost_per_1k.toFixed(4)}
                    </td>
                    <td className="py-2 text-right text-text-secondary">
                      ${m.output_cost_per_1k.toFixed(4)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-sm text-text-muted">No model pricing available.</p>
        )}
      </div>

      {/* Test Invoke */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <Send className="h-4 w-4" /> Test Invoke
        </h2>
        <textarea
          value={invokeInput}
          onChange={(e) => setInvokeInput(e.target.value)}
          placeholder="Enter a message to send to this adapter..."
          rows={4}
          className="w-full rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary p-3 focus:outline-none focus:ring-1 focus:ring-accent placeholder:text-text-muted font-mono"
        />
        <div className="mt-3 flex items-center justify-end">
          <button
            type="button"
            onClick={handleInvoke}
            disabled={isInvoking || !invokeInput.trim()}
            className="flex items-center gap-1.5 px-4 py-2 text-sm rounded-md bg-accent hover:bg-accent text-white disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isInvoking ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Send className="h-4 w-4" />
            )}
            {isInvoking ? 'Invoking...' : 'Send'}
          </button>
        </div>
        {invokeError && (
          <div className="mt-3 p-3 rounded border border-danger/30 bg-danger/10 text-danger text-sm">
            {invokeError}
          </div>
        )}
        {invokeResult && (
          <div className="mt-3 p-3 rounded border border-border-strong bg-surface-primary">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs text-text-secondary">Task: {invokeResult.task_id}</span>
              <span className="text-xs px-2 py-0.5 rounded bg-surface-tertiary text-text-secondary">
                {invokeResult.status}
              </span>
            </div>
            <pre className="text-xs text-text-primary font-mono whitespace-pre-wrap overflow-x-auto max-h-64 overflow-y-auto">
              {JSON.stringify(invokeResult, null, 2)}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}

function Field({ label, value, icon }: { label: string; value: string; icon?: React.ReactNode }) {
  return (
    <div>
      <div className="text-xs text-text-muted mb-1 flex items-center gap-1">
        {icon}
        {label}
      </div>
      <div className="text-text-primary break-all">{value}</div>
    </div>
  );
}
