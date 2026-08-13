// Metadata form + test-run control for NewScriptPage. Extracted from
// NewScriptPage.tsx to keep that file under the size gate.

import { Save, Loader2, CirclePlay, Tag } from 'lucide-react';
import { MonacoEditor } from '@/components/monaco-editor';
import { RUNTIME_OPTIONS } from './script-runtime-options';
import type { ScriptRuntime } from '@/lib/useScripts';
import type { Agent } from '@/lib/useAgents';

export interface MetadataFormSectionProps {
  name: string;
  description: string;
  runtime: ScriptRuntime;
  timeoutSecs: number;
  tagsInput: string;
  tags: string[];
  error: string | null;
  saving: boolean;
  running: boolean;
  agents: Agent[];
  targetAgentId: string;
  onName: (v: string) => void;
  onDescription: (v: string) => void;
  onRuntime: (v: ScriptRuntime) => void;
  onTimeout: (v: number) => void;
  onTagsInput: (v: string) => void;
  onTargetAgent: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  onTestRun: () => void;
}

export function MetadataFormSection({
  name,
  description,
  runtime,
  timeoutSecs,
  tagsInput,
  tags,
  error,
  saving,
  running,
  agents,
  targetAgentId,
  onName,
  onDescription,
  onRuntime,
  onTimeout,
  onTagsInput,
  onTargetAgent,
  onSubmit,
  onTestRun,
}: MetadataFormSectionProps) {
  return (
    <form
      onSubmit={onSubmit}
      className="lg:col-span-1 space-y-4 rounded-lg border border-slate-800 bg-slate-900 p-5"
    >
      <h2 className="text-sm font-semibold text-white">Metadata</h2>

      <div>
        <label className="block text-xs text-gray-300 mb-1">Name *</label>
        <input
          type="text"
          value={name}
          onChange={(e) => onName(e.target.value)}
          placeholder="e.g. Restart nginx service"
          className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        />
      </div>

      <div>
        <label className="block text-xs text-gray-300 mb-1">Description</label>
        <textarea
          value={description}
          onChange={(e) => onDescription(e.target.value)}
          rows={3}
          placeholder="What does this script do?"
          className="w-full px-3 py-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40 resize-none"
        />
      </div>

      <div>
        <label className="block text-xs text-gray-300 mb-1">Runtime *</label>
        <select
          value={runtime}
          onChange={(e) => onRuntime(e.target.value as ScriptRuntime)}
          className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        >
          {RUNTIME_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      <div>
        <label className="block text-xs text-gray-300 mb-1">Timeout (seconds)</label>
        <input
          type="number"
          min={5}
          max={3600}
          value={timeoutSecs}
          onChange={(e) => onTimeout(Math.max(5, Number(e.target.value) || 60))}
          className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        />
        <p className="text-xs text-gray-400 mt-1">5 – 3600 seconds</p>
      </div>

      <div>
        <label className="block text-xs text-gray-300 mb-1">
          <Tag className="inline h-3 w-3 mr-1" />
          Tags (comma-separated)
        </label>
        <input
          type="text"
          value={tagsInput}
          onChange={(e) => onTagsInput(e.target.value)}
          placeholder="maintenance, restart, nginx"
          className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
        />
        {tags.length > 0 && (
          <div className="flex flex-wrap gap-1 mt-2">
            {tags.map((t) => (
              <span
                key={t}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-slate-800 border border-slate-700 text-gray-300"
              >
                {t}
              </span>
            ))}
          </div>
        )}
      </div>

      {error && (
        <div className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error}
        </div>
      )}

      <div className="flex flex-col gap-2 pt-2">
        <button
          type="submit"
          disabled={saving}
          className="inline-flex items-center justify-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 text-sm text-white disabled:opacity-50 transition-colors"
        >
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          <span>{saving ? 'Saving…' : 'Save Script'}</span>
        </button>

        <div className="rounded-md border border-slate-800 p-3 space-y-2">
          <label className="block text-xs text-gray-300">Test Run target</label>
          <select
            value={targetAgentId}
            onChange={(e) => onTargetAgent(e.target.value)}
            className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
          >
            <option value="">Select an agent…</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.hostname || a.id} ({a.status})
              </option>
            ))}
          </select>
          <button
            type="button"
            disabled={running || !targetAgentId}
            onClick={() => void onTestRun()}
            className="inline-flex items-center justify-center gap-2 w-full px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 transition-colors"
          >
            {running ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <CirclePlay className="h-4 w-4" />
            )}
            <span>{running ? 'Starting…' : 'Test Run'}</span>
          </button>
          <p className="text-[11px] text-gray-400">
            Saves the script (if needed) and executes it on the selected agent.
          </p>
        </div>
      </div>
    </form>
  );
}
