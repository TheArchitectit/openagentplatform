import { createFileRoute, Link } from '@tanstack/react-router';
import { usePolicies, type PolicyCategory } from '@/lib/usePolicies';
import { useDashboard } from './useDashboard';
import { KpiCard } from './dashboard_components';

export const Route = createFileRoute('/dashboard')({ component: DashboardPage });

function DashboardPage() {
  const {
    checksLoading, alertsLoading, policiesLoading,
    greeting, agentKpis, checkRow, alertRow, patchKpis, scriptKpis,
    compliance, policies,
    activityItems, activityLoading, activityError,
  } = useDashboard();

  return (
    <div className="space-y-6" aria-busy={checksLoading || alertsLoading || policiesLoading}>
      {/* Header row */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Dashboard</h1>
          <p className="text-gray-400 text-sm mt-1">
            {greeting} — overview of your fleet, agents, and recent activity.
          </p>
        </div>
      </div>

      {/* Agents + Checks KPIs */}
      <div role="group" aria-label="Agent and check KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7 gap-4">
        {[...agentKpis, ...checkRow].map((kpi: any) => (
          <KpiCard key={kpi.label} kpi={kpi} />
        ))}
      </div>

      {/* Alert KPIs */}
      <section aria-labelledby="alerts-heading">
        <h2 id="alerts-heading" className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-3">Alerts</h2>
        <div role="group" aria-label="Alert KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {alertRow.map((kpi: any) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Patch KPIs */}
      <section aria-labelledby="patches-heading">
        <div className="flex items-center justify-between mb-3">
          <h2 id="patches-heading" className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Patches</h2>
          <Link to="/patches" aria-label="View all patches" className="text-xs text-gray-400 hover:text-white focus:outline-none focus-visible:underline transition-colors">
            View all →
          </Link>
        </div>
        <div role="group" aria-label="Patch KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {patchKpis.map((kpi: any) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Script KPIs */}
      <section aria-labelledby="scripts-heading">
        <div className="flex items-center justify-between mb-3">
          <h2 id="scripts-heading" className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Scripts</h2>
          <Link to="/scripts" aria-label="View all scripts" className="text-xs text-gray-400 hover:text-white focus:outline-none focus-visible:underline transition-colors">
            View all →
          </Link>
        </div>
        <div role="group" aria-label="Script KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {scriptKpis.map((kpi: any) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Bottom section: compliance + violations */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Policy compliance overall score */}
        <Link
          to="/policies"
          aria-label="View policy compliance details"
          className="rounded-xl border border-slate-800 bg-slate-900 p-5 hover:border-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors block"
        >
          <p className="text-sm text-gray-400">Overall compliance</p>
          <p
            className={
              'text-3xl font-bold mt-2 tabular-nums ' +
              (compliance.overallPct === null
                ? 'text-gray-500'
                : compliance.overallPct >= 80
                ? 'text-emerald-400'
                : compliance.overallPct >= 60
                ? 'text-amber-400'
                : 'text-red-400')
            }
            role="status"
            aria-label={
              compliance.overallPct === null
                ? 'Overall compliance: no data'
                : `Overall compliance: ${compliance.overallPct.toFixed(0)} percent`
            }
          >
            {policiesLoading && policies.length === 0
              ? '—'
              : compliance.overallPct === null
              ? '—'
              : `${compliance.overallPct.toFixed(0)}%`}
          </p>
          <p className="text-xs text-gray-500 mt-3">
            {policies.length} {policies.length === 1 ? 'policy' : 'policies'}
            {compliance.totalAgents > 0
              ? ` · ${compliance.compliantAgents} of ${compliance.totalAgents} agents compliant`
              : ''}
          </p>
        </Link>

        {/* Violations by category */}
        <div className="rounded-xl border border-slate-800 bg-slate-900 p-5 lg:col-span-2">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-sm font-semibold text-white">Violations by category</h3>
            <Link to="/policies" aria-label="View all policy violations" className="text-xs text-gray-400 hover:text-white focus:outline-none focus-visible:underline transition-colors">
              View all →
            </Link>
          </div>
          {policies.length === 0 ? (
            <div className="text-center text-xs text-gray-500 py-6" role="status">
              No policies to chart yet.
            </div>
          ) : (
            <div role="list" aria-label="Violations by policy category" className="space-y-2.5">
              {(Object.keys(compliance.byCategory) as PolicyCategory[]).map((cat) => {
                const { violations, total } = compliance.byCategory[cat];
                const pct = total > 0 ? (violations / total) * 100 : 0;
                if (total === 0) return null;
                return (
                  <div key={cat} role="listitem" className="flex items-center gap-3">
                    <div className="w-24 text-xs text-gray-400 capitalize">{cat}</div>
                    <div
                      className="flex-1 h-5 rounded bg-slate-800 overflow-hidden border border-slate-700"
                      role="progressbar"
                      aria-valuenow={Math.round(pct)}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-label={`${cat} violation rate`}
                    >
                      <div className="h-full bg-red-500/70 transition-all" style={{ width: `${pct}%` }} />
                    </div>
                    <div className="w-20 text-right text-xs text-gray-400 tabular-nums" aria-label={`${violations} of ${total} policies with violations`}>
                      {violations} / {total}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Recent Activity */}
      <section aria-labelledby="activity-heading" className="rounded-xl border border-slate-800 bg-slate-900 p-5">
        <div className="flex items-center justify-between mb-3">
          <h3 id="activity-heading" className="text-sm font-semibold text-white">Recent Activity</h3>
          <Link to="/settings/audit-log" aria-label="View full audit log" className="text-xs text-gray-400 hover:text-white focus:outline-none focus-visible:underline transition-colors">
            View all →
          </Link>
        </div>

        {activityLoading ? (
          <div role="status" aria-label="Loading recent activity" className="space-y-2.5">
            {[0, 1, 2, 3, 4].map((i) => (
              <div key={i} className="flex items-center gap-3 animate-pulse">
                <div className="h-4 w-4 rounded bg-slate-800" aria-hidden="true" />
                <div className="flex-1 space-y-1.5">
                  <div className="h-3 w-2/3 rounded bg-slate-800" aria-hidden="true" />
                  <div className="h-2.5 w-1/3 rounded bg-slate-800/60" aria-hidden="true" />
                </div>
                <div className="h-2.5 w-10 rounded bg-slate-800/60" aria-hidden="true" />
              </div>
            ))}
          </div>
        ) : activityError ? (
          <div className="text-center text-xs text-gray-500 py-6" role="status">Activity feed unavailable.</div>
        ) : activityItems.length === 0 ? (
          <div className="text-center text-xs text-gray-500 py-6" role="status">No recent activity.</div>
        ) : (
          <ul role="list" aria-label="Recent audit events" className="space-y-2.5">
            {activityItems.map((item: any) => {
              const toneColor =
                item.tone === 'success' ? 'text-emerald-400'
                  : item.tone === 'danger' ? 'text-red-400'
                  : item.tone === 'warning' ? 'text-amber-400'
                  : 'text-blue-400';
              const dotColor =
                item.tone === 'success' ? 'bg-emerald-500'
                  : item.tone === 'danger' ? 'bg-red-500'
                  : item.tone === 'warning' ? 'bg-amber-500'
                  : 'bg-blue-500';
              return (
                <li key={item.id} role="listitem" className="flex items-center gap-3">
                  <span className={`h-2 w-2 rounded-full shrink-0 ${dotColor}`} aria-hidden="true" />
                  <div className="flex-1 min-w-0">
                    <p className={`text-sm truncate ${toneColor}`}>{item.title}</p>
                    {item.meta ? <p className="text-xs text-gray-500 truncate">{item.meta}</p> : null}
                  </div>
                  <span className="text-xs text-gray-500 shrink-0 tabular-nums" aria-label={`${item.time}`}>{item.time}</span>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
