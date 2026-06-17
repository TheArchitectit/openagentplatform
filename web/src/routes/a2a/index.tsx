// A2A Dashboard — overview of A2A-protocol-compatible adapters and
// real-time task telemetry. The page renders a four-KPI summary bar
// at the top followed by a responsive grid of agent cards.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import {
  Bot,
  Activity,
  CheckCircle2,
  CircleDollarSign,
  Cpu,
  Zap,
  Radio,
  Search,
  RefreshCw,
} from 'lucide-react';
import { useA2AAdapters, fetchTasks, type A2AAdapter } from '@/lib/useA2A';

export const Route = createFileRoute('/a2a/')({
  component: A2ADashboardPage,
});

function shortId(id: string): string {
  if (!id) return '—';
  if (id.length <= 12) return id;
  return id.slice(0, 8);
}

function healthDot(h: A2AAdapter['health']): string {
  switch (h) {
    case 'healthy':
      return 'bg-success';
    case 'degraded':
      return 'bg-warning';
    case 'unhealthy':
      return 'bg-danger';
    default:
      return 'bg-text-muted';
  }
}

function A2ADashboardPage() {
  const { adapters, isLoading, error, refresh } = useA2AAdapters();
  const [taskStats, setTaskStats] = useState({
    total: 0,
    completed: 0,
    failed: 0,
    totalCost: 0,
  });
  const [search, setSearch] = useState('');
  const [now] = useState(() => Date.now());

  // Fetch task aggregates for the KPI bar.
  useEffect(() => {
    let cancelled = false;
    const today = new Date(now);
    today.setHours(0, 0, 0, 0);
    const todayIso = today.toISOString();
    void (async () => {
      try {
        const all = await fetchTasks({ limit: 500 });
        if (cancelled) return;
        const todays = all.filter((t) => new Date(t.created_at).getTime() >= today.getTime());
        const completed = todays.filter((t) => t.status === 'completed').length;
        const failed = todays.filter((t) => t.status === 'failed').length;
        const totalCost = todays.reduce((sum, t) => sum + (t.cost ?? 0), 0);
        setTaskStats({ total: todays.length, completed, failed, totalCost });
      } catch {
        /* non-fatal */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [now]);

  const activeAgents = useMemo(
    () => adapters.filter((a) => a.health === 'healthy').length,
    [adapters]
  );
  const successRate =
    taskStats.total > 0
      ? ((taskStats.completed / (taskStats.completed + taskStats.failed || 1)) * 100)
      : 0;

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim();
    if (!q) return adapters;
    return adapters.filter(
      (a) =>
        a.name.toLowerCase().includes(q) ||
        a.display_name?.toLowerCase().includes(q) ||
        a.provider?.toLowerCase().includes(q) ||
        a.skills?.some((s) => s.name.toLowerCase().includes(q) || s.tags?.some((t) => t.toLowerCase().includes(q)))
    );
  }, [adapters, search]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary flex items-center gap-2">
            <Radio className="h-6 w-6 text-accent" />
            A2A Dashboard
          </h1>
          <p className="text-sm text-text-secondary mt-1">
            Agent-to-Agent protocol — adapters, tasks, and cost analytics
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-border-strong bg-surface-tertiary hover:bg-border-strong text-text-primary"
        >
          <RefreshCw className="h-4 w-4" />
          Refresh
        </button>
      </div>

      {/* KPI bar */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <KpiCard
          icon={<Bot className="h-5 w-5" />}
          label="Active Agents"
          value={String(activeAgents)}
          sub={`${adapters.length} total`}
          accent="indigo"
        />
        <KpiCard
          icon={<Activity className="h-5 w-5" />}
          label="Tasks Today"
          value={String(taskStats.total)}
          sub={`${taskStats.completed} completed, ${taskStats.failed} failed`}
          accent="sky"
        />
        <KpiCard
          icon={<CheckCircle2 className="h-5 w-5" />}
          label="Success Rate"
          value={`${successRate.toFixed(1)}%`}
          sub="Completed vs failed"
          accent="emerald"
        />
        <KpiCard
          icon={<CircleDollarSign className="h-5 w-5" />}
          label="Total Cost Today"
          value={`$${taskStats.totalCost.toFixed(2)}`}
          sub="Across all adapters"
          accent="amber"
        />
      </div>

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" />
        <input
          type="text"
          placeholder="Search adapters, skills, or providers..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full pl-9 pr-3 py-2 rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
        />
      </div>

      {/* State messages */}
      {error && (
        <div className="p-3 rounded-md border border-danger/30 bg-danger/10 text-danger text-sm">
          Failed to load adapters: {error.message}
        </div>
      )}

      {/* Card grid */}
      {isLoading ? (
        <div className="text-center py-12 text-text-secondary text-sm">Loading adapters...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12 text-text-muted text-sm">No adapters found.</div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {filtered.map((a) => (
            <AdapterCard key={a.name} adapter={a} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function KpiCard({
  icon,
  label,
  value,
  sub,
  accent,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub: string;
  accent: 'indigo' | 'sky' | 'emerald' | 'amber';
}) {
  const accentMap: Record<typeof accent, string> = {
    indigo: 'text-accent bg-accent/10',
    sky: 'text-info bg-info/10',
    emerald: 'text-success bg-success/10',
    amber: 'text-warning bg-warning/10',
  };
  return (
    <div className="rounded-lg border border-border-subtle bg-surface-secondary p-4">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wider text-text-secondary">{label}</span>
        <span className={`p-1.5 rounded-md ${accentMap[accent]}`}>{icon}</span>
      </div>
      <div className="mt-2 text-2xl font-semibold text-text-primary">{value}</div>
      <div className="text-xs text-text-muted mt-1">{sub}</div>
    </div>
  );
}

function AdapterCard({ adapter }: { adapter: A2AAdapter }) {
  const dot = healthDot(adapter.health);
  return (
    <Link
      to="/a2a/agents/$name"
      params={{ name: adapter.name }}
      className="block rounded-lg border border-border-subtle bg-surface-secondary hover:border-accent/50 hover:bg-surface-tertiary/80 transition-colors p-4"
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <div className="h-9 w-9 rounded-md bg-surface-tertiary flex items-center justify-center text-accent">
            {adapter.icon ? (
              <span className="text-lg">{adapter.icon}</span>
            ) : (
              <Cpu className="h-5 w-5" />
            )}
          </div>
          <div>
            <div className="text-sm font-medium text-text-primary">
              {adapter.display_name ?? adapter.name}
            </div>
            <div className="text-xs text-text-muted">v{adapter.version}</div>
          </div>
        </div>
        <span className={`h-2.5 w-2.5 rounded-full ${dot} flex-shrink-0 mt-1`} title={adapter.health} />
      </div>
      {adapter.description && (
        <p className="mt-2 text-xs text-text-secondary line-clamp-2">{adapter.description}</p>
      )}
      <div className="mt-3 flex flex-wrap gap-1">
        {adapter.skills?.slice(0, 4).map((s) => (
          <span
            key={s.name}
            className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded bg-surface-tertiary text-text-secondary"
          >
            {s.name}
          </span>
        ))}
        {(adapter.skills?.length ?? 0) > 4 && (
          <span className="text-[10px] px-1.5 py-0.5 text-text-muted">
            +{adapter.skills!.length - 4}
          </span>
        )}
      </div>
      <div className="mt-3 flex items-center justify-between text-[11px] text-text-muted">
        <span className="flex items-center gap-1">
          <Zap className="h-3 w-3" />
          {adapter.streaming ? 'Streaming' : 'Sync only'}
        </span>
        {adapter.active_tasks !== undefined && (
          <span>{adapter.active_tasks} active</span>
        )}
      </div>
    </Link>
  );
}
