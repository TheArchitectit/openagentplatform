import { useMemo } from 'react';
import { Eye, Package, Server } from 'lucide-react';
import { type PatchCatalogItem } from '@/lib/usePatches';
import { type Agent } from '@/lib/useAgents';
import { SeverityBadge } from '@/components/severity-badge';

// ---------------------------------------------------------------------------
// ConfigureStep — job name, strategy, batch settings, reboot policy
// ---------------------------------------------------------------------------

export function ConfigureStep({
  name,
  onNameChange,
  description,
  onDescriptionChange,
  strategy,
  onStrategyChange,
  batchSize,
  onBatchSizeChange,
  batchIntervalMinutes,
  onBatchIntervalChange,
  rebootPolicy,
  onRebootPolicyChange,
  maintenanceStart,
  onMaintenanceStartChange,
  maintenanceEnd,
  onMaintenanceEndChange,
}: {
  name: string;
  onNameChange: (v: string) => void;
  description: string;
  onDescriptionChange: (v: string) => void;
  strategy: 'immediate' | 'staged' | 'maintenance_window';
  onStrategyChange: (v: 'immediate' | 'staged' | 'maintenance_window') => void;
  batchSize: number;
  onBatchSizeChange: (v: number) => void;
  batchIntervalMinutes: number;
  onBatchIntervalChange: (v: number) => void;
  rebootPolicy: 'never' | 'if_required' | 'always' | 'scheduled';
  onRebootPolicyChange: (v: 'never' | 'if_required' | 'always' | 'scheduled') => void;
  maintenanceStart: string;
  onMaintenanceStartChange: (v: string) => void;
  maintenanceEnd: string;
  onMaintenanceEndChange: (v: string) => void;
}) {
  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm font-medium text-white mb-1">Job name *</label>
        <input
          type="text"
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder="e.g. Q2 Critical Security Rollout"
          className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        />
      </div>
      <div>
        <label className="block text-sm font-medium text-white mb-1">Description</label>
        <textarea
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          rows={2}
          placeholder="Optional context for reviewers…"
          className="w-full px-3 py-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-white mb-1.5">Deployment strategy</label>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
          {(
            [
              { value: 'immediate', label: 'Immediate', desc: 'Roll out to all targets at once' },
              { value: 'staged', label: 'Staged', desc: '10% → 25% → 50% → 100%' },
              { value: 'maintenance_window', label: 'Maintenance', desc: 'Within a defined window' },
            ] as const
          ).map((s) => (
            <button
              key={s.value}
              type="button"
              onClick={() => onStrategyChange(s.value)}
              className={
                'text-left rounded-md border p-3 transition-colors ' +
                (strategy === s.value
                  ? 'border-blue-500/50 bg-blue-600/10'
                  : 'border-slate-800 bg-slate-900 hover:border-slate-700')
              }
            >
              <div className="text-sm font-medium text-white">{s.label}</div>
              <div className="text-xs text-gray-400 mt-0.5">{s.desc}</div>
            </button>
          ))}
        </div>
      </div>

      {strategy === 'staged' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium text-white mb-1">Batch size</label>
            <input
              type="number"
              min={1}
              value={batchSize}
              onChange={(e) => onBatchSizeChange(Math.max(1, Number(e.target.value) || 1))}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
            <p className="text-xs text-gray-400 mt-1">Agents per rollout stage.</p>
          </div>
          <div>
            <label className="block text-sm font-medium text-white mb-1">
              Batch interval (minutes)
            </label>
            <input
              type="number"
              min={1}
              value={batchIntervalMinutes}
              onChange={(e) =>
                onBatchIntervalChange(Math.max(1, Number(e.target.value) || 1))
              }
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
            <p className="text-xs text-gray-400 mt-1">Time between stages.</p>
          </div>
        </div>
      )}

      {strategy === 'maintenance_window' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium text-white mb-1">Window start</label>
            <input
              type="datetime-local"
              value={maintenanceStart}
              onChange={(e) => onMaintenanceStartChange(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-white mb-1">Window end</label>
            <input
              type="datetime-local"
              value={maintenanceEnd}
              onChange={(e) => onMaintenanceEndChange(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
          </div>
        </div>
      )}

      <div>
        <label className="block text-sm font-medium text-white mb-1.5">Reboot policy</label>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          {(
            [
              { value: 'never', label: 'Never' },
              { value: 'if_required', label: 'If Required' },
              { value: 'always', label: 'Always' },
              { value: 'scheduled', label: 'Scheduled' },
            ] as const
          ).map((r) => (
            <button
              key={r.value}
              type="button"
              onClick={() => onRebootPolicyChange(r.value)}
              className={
                'rounded-md border px-3 h-8 text-sm transition-colors ' +
                (rebootPolicy === r.value
                  ? 'border-blue-500/50 bg-blue-600/10 text-white'
                  : 'border-slate-800 bg-slate-900 text-gray-300 hover:border-slate-700')
              }
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ReviewStep — final review before submission
// ---------------------------------------------------------------------------

export function ReviewStep({
  patchCount,
  agentCount,
  name,
  description,
  strategy,
  batchSize,
  batchIntervalMinutes,
  rebootPolicy,
  maintenanceStart,
  maintenanceEnd,
  catalog,
  selectedPatchIds,
  agents,
  selectedAgentIds,
}: {
  patchCount: number;
  agentCount: number;
  name: string;
  description: string;
  strategy: 'immediate' | 'staged' | 'maintenance_window';
  batchSize: number;
  batchIntervalMinutes: number;
  rebootPolicy: 'never' | 'if_required' | 'always' | 'scheduled';
  maintenanceStart: string;
  maintenanceEnd: string;
  catalog: PatchCatalogItem[];
  selectedPatchIds: string[];
  agents: Agent[];
  selectedAgentIds: string[];
}) {
  const patchDetails = useMemo(
    () => catalog.filter((c) => selectedPatchIds.includes(c.id)),
    [catalog, selectedPatchIds]
  );
  const agentDetails = useMemo(
    () => agents.filter((a) => selectedAgentIds.includes(a.id)),
    [agents, selectedAgentIds]
  );

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-slate-800 bg-slate-900 p-4 space-y-2">
        <h3 className="text-sm font-semibold text-white flex items-center gap-2">
          <Eye className="h-4 w-4 text-gray-300" />
          Job summary
        </h3>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
          <div>
            <dt className="text-xs text-gray-400">Name</dt>
            <dd className="text-white">{name || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400">Strategy</dt>
            <dd className="text-white capitalize">{strategy.replace('_', ' ')}</dd>
          </div>
          {description && (
            <div className="sm:col-span-2">
              <dt className="text-xs text-gray-400">Description</dt>
              <dd className="text-white">{description}</dd>
            </div>
          )}
          {strategy === 'staged' && (
            <>
              <div>
                <dt className="text-xs text-gray-400">Batch size</dt>
                <dd className="text-white">{batchSize}</dd>
              </div>
              <div>
                <dt className="text-xs text-gray-400">Batch interval</dt>
                <dd className="text-white">{batchIntervalMinutes} min</dd>
              </div>
            </>
          )}
          {strategy === 'maintenance_window' && (
            <>
              <div>
                <dt className="text-xs text-gray-400">Window start</dt>
                <dd className="text-white">{maintenanceStart || '—'}</dd>
              </div>
              <div>
                <dt className="text-xs text-gray-400">Window end</dt>
                <dd className="text-white">{maintenanceEnd || '—'}</dd>
              </div>
            </>
          )}
          <div>
            <dt className="text-xs text-gray-400">Reboot policy</dt>
            <dd className="text-white capitalize">{rebootPolicy.replace('_', ' ')}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400">Patches / Agents</dt>
            <dd className="text-white tabular-nums">
              {patchCount} / {agentCount}
            </dd>
          </div>
        </dl>
      </div>

      <div className="rounded-md border border-slate-800 bg-slate-900">
        <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-white flex items-center gap-2">
            <Package className="h-4 w-4 text-gray-300" />
            Patches
          </h3>
          <span className="text-xs text-gray-400">{patchCount} selected</span>
        </div>
        <ul className="divide-y divide-slate-800 max-h-48 overflow-y-auto">
          {patchDetails.length === 0 ? (
            <li className="px-4 py-3 text-sm text-gray-400">No patches selected.</li>
          ) : (
            patchDetails.map((p) => (
              <li key={p.id} className="px-4 py-2 flex items-center gap-3 text-sm">
                <SeverityBadge severity={p.severity} showLabel={false} />
                <div className="flex-1 min-w-0">
                  <p className="text-white truncate">{p.title}</p>
                  <p className="text-xs text-gray-400">
                    {p.kb_number ?? '—'}
                    {p.os ? ` · ${p.os}` : ''}
                  </p>
                </div>
              </li>
            ))
          )}
        </ul>
      </div>

      <div className="rounded-md border border-slate-800 bg-slate-900">
        <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-white flex items-center gap-2">
            <Server className="h-4 w-4 text-gray-300" />
            Target agents
          </h3>
          <span className="text-xs text-gray-400">{agentCount} selected</span>
        </div>
        <ul className="divide-y divide-slate-800 max-h-48 overflow-y-auto">
          {agentDetails.length === 0 ? (
            <li className="px-4 py-3 text-sm text-gray-400">No agents selected.</li>
          ) : (
            agentDetails.map((a) => (
              <li key={a.id} className="px-4 py-2 flex items-center gap-3 text-sm">
                <span className="text-white truncate flex-1">{a.hostname || a.id}</span>
                <span className="text-xs text-gray-400">{a.os || '—'}</span>
                <span
                  className={
                    'inline-flex px-2 py-0.5 rounded-full border text-xs ' +
                    (a.status === 'online'
                      ? 'bg-green-500/10 text-green-400 border-green-800'
                      : 'bg-slate-700/30 text-gray-300 border-slate-700/30')
                  }
                >
                  {a.status || 'unknown'}
                </span>
              </li>
            ))
          )}
        </ul>
      </div>
    </div>
  );
}
