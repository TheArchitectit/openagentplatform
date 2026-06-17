// A2A Cost Dashboard — date-range cost analytics with KPIs, daily bar
// chart, breakdown by adapter and model, and per-org budget progress.

import { createFileRoute } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import {
  CircleDollarSign,
  Calendar,
  TrendingUp,
  Layers,
  Cpu,
  Building2,
  AlertTriangle,
} from 'lucide-react';
import { useA2ACost } from '@/lib/useA2A';

export const Route = createFileRoute('/a2a/costs')({
  component: CostDashboardPage,
});

function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
}

function thirtyDaysAgoIso(): string {
  const d = new Date();
  d.setDate(d.getDate() - 29);
  return d.toISOString().slice(0, 10);
}

function fmt(n: number): string {
  return n.toLocaleString();
}

function fmtMoney(n: number, currency = 'USD'): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(n);
}

function fmtPct(n: number): string {
  return `${n.toFixed(1)}%`;
}

function CostDashboardPage() {
  const [start, setStart] = useState(thirtyDaysAgoIso());
  const [end, setEnd] = useState(todayIso());
  const { summary, isLoading, error, refresh } = useA2ACost({
    start: new Date(start).toISOString(),
    end: new Date(end + 'T23:59:59').toISOString(),
  });

  const dailyMax = useMemo(() => {
    if (!summary?.by_day?.length) return 0;
    return Math.max(...summary.by_day.map((d) => d.cost), 0);
  }, [summary]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary flex items-center gap-2">
            <CircleDollarSign className="h-6 w-6 text-warning" />
            Cost Dashboard
          </h1>
          <p className="text-sm text-text-secondary mt-1">A2A protocol spend analytics</p>
        </div>
      </div>

      {/* Date range picker */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-4 flex flex-wrap items-end gap-3">
        <div>
          <label className="block text-xs text-text-muted mb-1">Start date</label>
          <input
            type="date"
            value={start}
            max={end}
            onChange={(e) => setStart(e.target.value)}
            className="rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary px-2 py-1.5"
          />
        </div>
        <div>
          <label className="block text-xs text-text-muted mb-1">End date</label>
          <input
            type="date"
            value={end}
            min={start}
            max={todayIso()}
            onChange={(e) => setEnd(e.target.value)}
            className="rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary px-2 py-1.5"
          />
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          className="px-3 py-1.5 text-sm rounded-md bg-accent hover:bg-accent text-white"
        >
          Apply
        </button>
        <div className="ml-auto flex items-center gap-2 text-xs text-text-muted">
          <Calendar className="h-3.5 w-3.5" />
          {summary?.date_range?.start
            ? `${summary.date_range.start.slice(0, 10)} → ${summary.date_range.end.slice(0, 10)}`
            : `${start} → ${end}`}
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-md border border-danger/30 bg-danger/10 text-danger text-sm">
          Failed to load cost data: {error.message}
        </div>
      )}

      {isLoading ? (
        <div className="text-center py-12 text-text-secondary text-sm">Loading cost data...</div>
      ) : summary ? (
        <>
          {/* Total cost KPI */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
            <div className="text-xs uppercase tracking-wider text-text-muted mb-1">
              Total cost for period
            </div>
            <div className="text-3xl font-semibold text-text-primary">
              {fmtMoney(summary.total_cost, summary.currency)}
            </div>
            <div className="text-xs text-text-muted mt-1 flex items-center gap-1">
              <TrendingUp className="h-3 w-3" />
              Across {summary.by_adapter?.reduce((s, a) => s + a.tasks, 0) ?? 0} tasks
            </div>
          </div>

          {/* Daily cost bar chart */}
          {summary.by_day && summary.by_day.length > 0 && (
            <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
              <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-4">
                Daily cost
              </h2>
              <div className="flex items-end gap-1 h-32">
                {summary.by_day.map((d) => {
                  const heightPct = dailyMax > 0 ? Math.max(2, (d.cost / dailyMax) * 100) : 0;
                  return (
                    <div
                      key={d.date}
                      className="flex-1 flex flex-col items-center justify-end group relative"
                      title={`${d.date}: ${fmtMoney(d.cost, summary.currency)}`}
                    >
                      <div
                        className="w-full bg-accent/70 hover:bg-accent-hover rounded-t transition-colors"
                        style={{ height: `${heightPct}%` }}
                      />
                      <div className="text-[9px] text-text-muted mt-1 truncate w-full text-center">
                        {d.date.slice(5)}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Cost by adapter */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
            <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
              <Layers className="h-4 w-4" /> Cost by Adapter
            </h2>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase text-text-muted border-b border-border-subtle">
                    <th className="py-2 pr-3">Adapter</th>
                    <th className="py-2 pr-3 text-right">Tasks</th>
                    <th className="py-2 pr-3 text-right">Tokens</th>
                    <th className="py-2 pr-3 text-right">Cost</th>
                    <th className="py-2 text-right">% of total</th>
                  </tr>
                </thead>
                <tbody>
                  {summary.by_adapter?.length ? (
                    summary.by_adapter.map((a) => (
                      <tr key={a.adapter} className="border-b border-border-subtle/50">
                        <td className="py-2 pr-3 text-text-primary font-mono">{a.adapter}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">{fmt(a.tasks)}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">{fmt(a.tokens)}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">
                          {fmtMoney(a.cost, summary.currency)}
                        </td>
                        <td className="py-2 text-right text-text-secondary">
                          <div className="flex items-center justify-end gap-2">
                            <div className="w-20 h-1.5 bg-surface-tertiary rounded overflow-hidden">
                              <div
                                className="h-full bg-accent"
                                style={{ width: `${Math.min(100, a.percent_of_total)}%` }}
                              />
                            </div>
                            <span className="w-12 text-right">{fmtPct(a.percent_of_total)}</span>
                          </div>
                        </td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={5} className="py-4 text-center text-text-muted text-xs">
                        No adapter data for this period.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {/* Cost by model */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
            <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
              <Cpu className="h-4 w-4" /> Cost by Model
            </h2>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase text-text-muted border-b border-border-subtle">
                    <th className="py-2 pr-3">Model</th>
                    <th className="py-2 pr-3 text-right">Tasks</th>
                    <th className="py-2 pr-3 text-right">Tokens</th>
                    <th className="py-2 pr-3 text-right">Cost</th>
                    <th className="py-2 text-right">% of total</th>
                  </tr>
                </thead>
                <tbody>
                  {summary.by_model?.length ? (
                    summary.by_model.map((m) => (
                      <tr key={m.model} className="border-b border-border-subtle/50">
                        <td className="py-2 pr-3 text-text-primary font-mono">{m.model}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">{fmt(m.tasks)}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">{fmt(m.tokens)}</td>
                        <td className="py-2 pr-3 text-right text-text-secondary">
                          {fmtMoney(m.cost, summary.currency)}
                        </td>
                        <td className="py-2 text-right text-text-secondary">{fmtPct(m.percent_of_total)}</td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={5} className="py-4 text-center text-text-muted text-xs">
                        No model data for this period.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {/* Budget progress per org */}
          {summary.by_org && summary.by_org.length > 0 && (
            <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
              <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
                <Building2 className="h-4 w-4" /> Budget by Organization
              </h2>
              <div className="space-y-3">
                {summary.by_org.map((o) => (
                  <BudgetBar key={o.org_id} org={o} currency={summary.currency} />
                ))}
              </div>
            </div>
          )}
        </>
      ) : null}
    </div>
  );
}

function BudgetBar({ org, currency }: { org: { org_id: string; org_name?: string; spend: number; budget: number; percent_used: number; status: 'ok' | 'warning' | 'critical' | 'exceeded' }; currency: string }) {
  const colorMap: Record<typeof org.status, string> = {
    ok: 'bg-success',
    warning: 'bg-warning',
    critical: 'bg-orange-500',
    exceeded: 'bg-danger',
  };
  // Add visual threshold markers at 80% and 90%.
  const fillPct = Math.min(100, Math.max(0, org.percent_used));
  return (
    <div>
      <div className="flex items-center justify-between text-sm mb-1">
        <span className="text-text-primary font-medium">
          {org.org_name ?? org.org_id}
          {org.status === 'exceeded' && (
            <AlertTriangle className="inline h-3.5 w-3.5 ml-1.5 text-danger" />
          )}
        </span>
        <span className="text-text-secondary text-xs">
          {fmtMoney(org.spend, currency)} / {fmtMoney(org.budget, currency)}
          <span className="ml-2 text-text-secondary">{fmtPct(org.percent_used)}</span>
        </span>
      </div>
      <div className="relative h-2.5 bg-surface-tertiary rounded overflow-hidden">
        <div
          className={`h-full ${colorMap[org.status]} transition-all`}
          style={{ width: `${fillPct}%` }}
        />
        {/* Threshold tick marks at 80% and 90% */}
        <div className="absolute top-0 bottom-0 w-px bg-warning/40" style={{ left: '80%' }} />
        <div className="absolute top-0 bottom-0 w-px bg-orange-300/40" style={{ left: '90%' }} />
      </div>
    </div>
  );
}
