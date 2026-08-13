import { type PatchCatalogItem } from '@/lib/usePatches';

export { ReviewStep } from './ReviewStep';

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
