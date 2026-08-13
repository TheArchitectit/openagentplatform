// Alert detail — non-interactive view blocks (timestamps + details card).
// Pulled out of $alertId.tsx to keep that file under the size gate.

import { Link } from '@tanstack/react-router';
import {
  Activity,
  Bot,
} from 'lucide-react';
import { formatTime } from './alert_detail_components';
import { type Alert } from '@/lib/useAlerts';

export function AlertDetailView({ alert }: { alert: Alert }) {
  const state = (alert.state ?? 'open').toLowerCase();

  return (
    <>
      {/* Timestamps row */}
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Created</dt>
            <dd className="text-white mt-1">{formatTime(alert.created_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">
              Last state change
            </dt>
            <dd className="text-white mt-1">
              {formatTime(alert.updated_at ?? alert.created_at)}
            </dd>
          </div>
          {alert.acknowledged_at && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Acknowledged
              </dt>
              <dd className="text-white mt-1">
                {formatTime(alert.acknowledged_at)}
                {alert.acknowledged_by && (
                  <span className="text-gray-400"> · {alert.acknowledged_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.resolved_at && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Resolved
              </dt>
              <dd className="text-white mt-1">
                {formatTime(alert.resolved_at)}
                {alert.resolved_by && (
                  <span className="text-gray-400"> · {alert.resolved_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.snoozed_until && state === 'snoozed' && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Snoozed until
              </dt>
              <dd className="text-white mt-1">{formatTime(alert.snoozed_until)}</dd>
            </div>
          )}
        </dl>
      </div>

      {/* Details card */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800">
          <h2 className="text-sm font-semibold text-white">Details</h2>
        </div>
        <div className="p-5 space-y-5">
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">Check</dt>
              <dd className="text-white mt-1 flex items-center gap-2">
                <Activity className="h-3.5 w-3.5 text-gray-400" />
                {alert.check_name ? (
                  alert.check_id ? (
                    <Link
                      to="/checks/$checkId"
                      params={{ checkId: alert.check_id }}
                      className="text-white hover:text-blue-400 underline-offset-2 hover:underline"
                    >
                      {alert.check_name}
                    </Link>
                  ) : (
                    <span>{alert.check_name}</span>
                  )
                ) : alert.check_id ? (
                  <span className="font-mono text-xs">{alert.check_id}</span>
                ) : (
                  <span className="text-gray-400">—</span>
                )}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">Agent</dt>
              <dd className="text-white mt-1 flex items-center gap-2">
                <Bot className="h-3.5 w-3.5 text-gray-400" />
                {alert.hostname ? (
                  alert.agent_id ? (
                    <Link
                      to="/agents/$agentId"
                      params={{ agentId: alert.agent_id }}
                      className="text-white hover:text-blue-400 underline-offset-2 hover:underline"
                    >
                      {alert.hostname}
                    </Link>
                  ) : (
                    <span>{alert.hostname}</span>
                  )
                ) : alert.agent_id ? (
                  <span className="font-mono text-xs">{alert.agent_id}</span>
                ) : (
                  <span className="text-gray-400">—</span>
                )}
              </dd>
            </div>
            {alert.source && (
              <div>
                <dt className="text-xs text-gray-400 uppercase tracking-wider">
                  Source
                </dt>
                <dd className="text-white mt-1 font-mono text-xs">
                  {alert.source}
                </dd>
              </div>
            )}
            {alert.tags && alert.tags.length > 0 && (
              <div>
                <dt className="text-xs text-gray-400 uppercase tracking-wider">Tags</dt>
                <dd className="mt-1 flex flex-wrap gap-1.5">
                  {alert.tags.map((t) => (
                    <span
                      key={t}
                      className="inline-flex px-2 py-0.5 rounded-full bg-slate-800 border border-slate-700 text-xs text-gray-300"
                    >
                      {t}
                    </span>
                  ))}
                </dd>
              </div>
            )}
          </dl>

          {alert.output && (
            <div>
              <h3 className="text-xs text-gray-400 uppercase tracking-wider mb-1.5">
                Check output
              </h3>
              <pre className="rounded-md bg-slate-950/80 border border-slate-800 p-3 text-xs text-white font-mono whitespace-pre-wrap break-words max-h-72 overflow-auto">
                {alert.output}
              </pre>
            </div>
          )}

          {alert.metrics && Object.keys(alert.metrics).length > 0 && (
            <div>
              <h3 className="text-xs text-gray-400 uppercase tracking-wider mb-1.5">
                Metrics
              </h3>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
                {Object.entries(alert.metrics).map(([k, v]) => (
                  <div
                    key={k}
                    className="rounded-md border border-slate-800 bg-slate-800 px-3 py-2"
                  >
                    <div className="text-xs text-gray-400">{k}</div>
                    <div className="text-sm text-white font-medium tabular-nums">
                      {String(v)}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
