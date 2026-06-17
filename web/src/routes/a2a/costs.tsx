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

function CostDashboardPage() {
  const [start, setStart] = useState(thirtyDaysAgoIso());
  const [end, setEnd] = useState(todayIso());
  const { data, isLoading, error, refetch } = useA2ACost({ start, end });

  const totals = useMemo(() => {
    if (!data) return null;
    const total = data.by_day.reduce((s, d) => s + d.cost, 0);
    const tokens = data.by_day.reduce((s, d) => s + d.input_tokens + d.output_tokens, 0);
    const calls = data.by_day.reduce((s, d) => s + d.call_count, 0);
    const avgDaily = data.by_day.length > 0 ? total / data.by_day.length : 0;
    return { total, tokens, calls, avgDaily };
  }, [data]);

  // Build simple bar chart from daily costs
  const maxDaily = useMemo(() => {
    if (!data) return 0;
    return Math.max(0, ...data.by_day.map((d) => d.cost));
  }, [data]);

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
            <CircleDollarSign className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Cost Analytics</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              A2A spend by day, adapter, model, and organisation
            </p>
          </div>
        </div>
      </div>

      {/* Date range filter */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1.5">
          <Calendar className="h-3.5 w-3.5 text-gray-400" />
          <label htmlFor="cost-start" className="text-xs text-gray-300">
            From
          </label>
          <input
            id="cost-start"
            type="date"
            value={start}
            max={end}
            onChange={(e) => setStart(e.target.value)}
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <div className="flex items-center gap-1.5">
          <label htmlFor="cost-end" className="text-xs text-gray-300">
            To
          </label>
          <input
            id="cost-end"
            type="date"
            value={end}
            min={start}
            max={todayIso()}
            onChange={(e) => setEnd(e.target.value)}
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <button
          type="button"
          onClick={() => void refetch()}
          className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-blue-600 hover:bg-blue-500 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          Apply
        </button>
      </div>

      {error && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error.message}
        </div>
      )}

      {/* KPI bar */}
      {totals && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <KpiCard
            icon={<CircleDollarSign className="h-4 w-4" />}
            label="Total Cost"
            value={fmtMoney(totals.total)}
            sub={`${data?.by_day.length ?? 0} days`}
            accent="indigo"
          />
          <KpiCard
            icon={<TrendingUp className="h-4 w-4" />}
            label="Avg / Day"
            value={fmtMoney(totals.avgDaily)}
            sub="Daily average"
            accent="sky"
          />
          <KpiCard
            icon={<Layers className="h-4 w-4" />}
            label="Total Tokens"
            value={fmt(totals.tokens)}
            sub="Input + output"
            accent="emerald"
          />
          <KpiCard
            icon={<Cpu className="h-4 w-4" />}
            label="Total Calls"
            value={fmt(totals.calls)}
            sub="Task invocations"
            accent="amber"
          />
        </div>
      )}

      {/* Daily cost bar chart */}
      {data && data.by_day.length > 0 && (
        <section className="rounded-lg border border-slate-800 bg-slate-900 p-4" aria-label="Daily cost chart">
          <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Daily Cost</h2>
          <div className="flex items-end gap-1 h-40">
            {data.by_day.map((d) => {
              const h = maxDaily > 0 ? Math.max(2, (d.cost / maxDaily) * 100) : 0;
              return (
                <div
                  key={d.date}
                  className="flex-1 min-w-0 flex flex-col items-center justify-end"
                  title={`${d.date}: ${fmtMoney(d.cost)}`}
                >
                  <div
                    className="w-full rounded-t bg-blue-500/70 hover:bg-blue-500 transition-colors"
                    style={{ height: `${h}%` }}
                  />
                  <span className="text-[9px] text-gray-400 mt-0.5 truncate w-full text-center">
                    {d.date.slice(5)}
                  </span>
                </div>
              );
            })}
          </div>
        </section>
      )}

      {/* By adapter */}
      {data && data.by_adapter && data.by_adapter.length > 0 && (
        <section className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden" aria-label="Cost by adapter">
          <div className="px-4 py-3 border-b border-slate-800">
            <h2 className="text-sm font-semibold text-white uppercase tracking-wider flex items-center gap-2">
              <Layers className="h-4 w-4" /> By Adapter
            </h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                  <th className="px-4 py-2.5 font-medium">Adapter</th>
                  <th className="px-4 py-2.5 font-medium text-right">Cost</th>
                  <th className="px-4 py-2.5 font-medium text-right">Tokens</th>
                  <th className="px-4 py-2.5 font-medium text-right">Calls</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800">
                {data.by_adapter.map((row) => (
                  <tr key={row.adapter} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5 text-white text-xs font-mono">{row.adapter}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmtMoney(row.cost)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmt(row.input_tokens + row.output_tokens)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmt(row.call_count)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {/* By model */}
      {data && data.by_model && data.by_model.length > 0 && (
        <section className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden" aria-label="Cost by model">
          <div className="px-4 py-3 border-b border-slate-800">
            <h2 className="text-sm font-semibold text-white uppercase tracking-wider flex items-center gap-2">
              <Cpu className="h-4 w-4" /> By Model
            </h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                  <th className="px-4 py-2.5 font-medium">Model</th>
                  <th className="px-4 py-2.5 font-medium text-right">Cost</th>
                  <th className="px-4 py-2.5 font-medium text-right">Tokens</th>
                  <th className="px-4 py-2.5 font-medium text-right">Calls</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800">
                {data.by_model.map((row) => (
                  <tr key={row.model} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5 text-white text-xs font-mono">{row.model}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmtMoney(row.cost)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmt(row.input_tokens + row.output_tokens)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {fmt(row.call_count)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {/* By organisation / budgets */}
      {data && data.by_org && data.by_org.length > 0 && (
        <section className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden" aria-label="Budget progress by organisation">
          <div className="px-4 py-3 border-b border-slate-800">
            <h2 className="text-sm font-semibold text-white uppercase tracking-wider flex items-center gap-2">
              <Building2 className="h-4 w-4" /> Organisation Budgets
            </h2>
          </div>
          <div className="divide-y divide-slate-800">
            {data.by_org.map((row) => {
              const pct = row.budget_usd > 0 ? Math.min(100, (row.spent_usd / row.budget_usd) * 100) : 0;
              const overBudget = row.budget_usd > 0 && row.spent_usd > row.budget_usd;
              return (
                <div key={row.org} className="px-4 py-3 hover:bg-slate-800/40 transition-colors">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-white font-medium">{row.org}</span>
                    <span className="text-gray-300 text-xs">
                      {fmtMoney(row.spent_usd)} / {fmtMoney(row.budget_usd)}
                    </span>
                  </div>
                  <div className="mt-1.5 h-1.5 rounded-full bg-slate-800 overflow-hidden">
                    <div
                      className={`h-full rounded-full transition-all ${
                        overBudget
                          ? 'bg-red-500'
                          : pct > 80
                            ? 'bg-yellow-500'
                            : 'bg-green-500'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  {overBudget && (
                    <div className="mt-1 flex items-center gap-1 text-[10px] text-red-400">
                      <AlertTriangle className="h-3 w-3" />
                      Over budget
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </section>
      )}

      {/* Empty state */}
      {!isLoading && data && data.by_day.length === 0 && (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status">
          No cost data for the selected range.
        </div>
      )}
      {isLoading && !data && (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status" aria-live="polite">
          Loading cost data…
        </div>
      )}
    </div>
  );
}

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
    indigo: 'text-blue-400 bg-blue-500/10',
    sky: 'text-sky-400 bg-sky-500/10',
    emerald: 'text-green-400 bg-green-500/10',
    amber: 'text-yellow-400 bg-yellow-500/10',
  };
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wider text-gray-300">{label}</span>
        <span className={`p-1.5 rounded-md ${accentMap[accent]}`}>{icon}</span>
      </div>
      <div className="mt-2 text-2xl font-semibold text-white">{value}</div>
      <div className="text-xs text-gray-400 mt-1">{sub}</div>
    </div>
  );
}
