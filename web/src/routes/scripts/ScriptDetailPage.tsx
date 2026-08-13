// Script detail page component — view, edit, and run a script.
//
// Sections:
//   • Script info card: name, description, runtime, timeout, tags, timestamps
//   • Monaco code editor — read-only viewer by default; toggle to edit mode
//     (loads from jsDelivr CDN at runtime, falls back to textarea if offline)
//   • Run history table
//   • "Run Now" action — select target agent(s) and execute
//
// Edit mode PATCHES the script on save.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import {
  ArrowLeft,
  FileCode2,
  Save,
  CirclePlay,
  Loader2,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  Play,
  Edit3,
  Eye,
  Trash2,
  Tag,
  Terminal,
  Code2,
  Braces,
  Globe,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  useScripts,
  type Script,
  type ScriptRuntime,
  type ScriptRun,
  type ScriptRunStatus,
} from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';
import { RunNowModal, formatTime, formatDateTime } from './script_helpers';
import { RUNTIME_META, STATUS_META, RunHistoryTable } from './scriptDetail.meta';

export function ScriptDetailPage() {
  const { scriptId } = useParams({ from: '/scripts/$scriptId' });
  const navigate = useNavigate();
  const scripts = useScripts();
  const { agents } = useAgents();
  const [script, setScript] = useState<Script | null>(null);
  const [runs, setRuns] = useState<ScriptRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Script | null>(null);
  const [saving, setSaving] = useState(false);
  const [showRunModal, setShowRunModal] = useState(false);
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const s = await scripts.fetchScript(scriptId);
      setScript(s);
      setDraft(s);
      try {
        setRuns(await scripts.fetchRuns(scriptId, 20));
      } catch {
        setRuns([]);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [scriptId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    load();
  }, [load]);

  const meta = useMemo(
    () => (script ? RUNTIME_META[script.runtime] : undefined),
    [script]
  );

  const onSave = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const updated = await scripts.updateScript(scriptId, {
        name: draft.name,
        description: draft.description,
        runtime: draft.runtime,
        content: draft.content,
        timeout_secs: draft.timeout_secs,
        tags: draft.tags,
      });
      setScript(updated);
      setDraft(updated);
      setEditing(false);
      toast.success('Script saved');
    } catch (e) {
      toast.error('Save failed', { description: e instanceof Error ? e.message : String(e) });
    } finally {
      setSaving(false);
    }
  };

  const onDelete = async () => {
    if (!confirm('Delete this script? This cannot be undone.')) return;
    try {
      await scripts.deleteScript(scriptId);
      toast.success('Script deleted');
      navigate({ to: '/scripts' });
    } catch (e) {
      toast.error('Delete failed', { description: e instanceof Error ? e.message : String(e) });
    }
  };

  const onRun = async () => {
    if (!script || selectedAgents.size === 0) return;
    try {
      await scripts.runScript(scriptId, [...selectedAgents]);
      toast.success(`Run started on ${selectedAgents.size} agent(s)`);
      setShowRunModal(false);
      setSelectedAgents(new Set());
      setRuns(await scripts.fetchRuns(scriptId, 20));
    } catch (e) {
      toast.error('Run failed', { description: e instanceof Error ? e.message : String(e) });
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64 text-slate-400 gap-2">
        <Loader2 className="h-5 w-5 animate-spin" />
        <span>Loading script…</span>
      </div>
    );
  }

  if (error || !script || !draft || !meta) {
    return (
      <div className="max-w-3xl mx-auto py-8">
        <button
          onClick={() => navigate({ to: '/scripts' })}
          className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-white mb-4"
        >
          <ArrowLeft className="h-4 w-4" /> Back to scripts
        </button>
        <div className="rounded-lg border border-red-800 bg-red-500/10 p-4 text-red-300">
          {error ?? 'Script not found.'}
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto py-6 space-y-6">
      <div className="flex items-center justify-between">
        <button
          onClick={() => navigate({ to: '/scripts' })}
          className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-white"
        >
          <ArrowLeft className="h-4 w-4" /> Scripts
        </button>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowRunModal(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white transition-colors"
          >
            <Play className="h-4 w-4" /> Run Now
          </button>
          {editing ? (
            <>
              <button
                onClick={() => { setDraft(script); setEditing(false); }}
                className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-white hover:bg-slate-700"
              >
                Cancel
              </button>
              <button
                onClick={onSave}
                disabled={saving}
                className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-green-600 hover:bg-green-500 text-sm text-white disabled:opacity-50"
              >
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />} Save
              </button>
            </>
          ) : (
            <>
              <button
                onClick={() => setEditing(true)}
                className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-white hover:bg-slate-700"
              >
                <Edit3 className="h-4 w-4" /> Edit
              </button>
              <button
                onClick={onDelete}
                className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-red-800 bg-red-900/40 text-sm text-red-300 hover:bg-red-900/60"
              >
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            </>
          )}
        </div>
      </div>

      {/* Info card */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-5 space-y-4">
        <div className="flex items-center gap-3">
          <FileCode2 className="h-5 w-5 text-slate-400" />
          {editing ? (
            <input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              className="flex-1 bg-slate-800 border border-slate-700 rounded-md px-3 h-9 text-white text-lg font-semibold"
            />
          ) : (
            <h1 className="text-lg font-semibold text-white flex-1">{script.name}</h1>
          )}
          <span className={`inline-flex items-center gap-1 px-2 py-1 rounded text-xs border ${meta.classes}`}>
            <meta.icon className="h-3 w-3" /> {meta.label}
          </span>
        </div>
        {editing ? (
          <input
            value={draft.description ?? ''}
            onChange={(e) => setDraft({ ...draft, description: e.target.value })}
            placeholder="Description"
            className="w-full bg-slate-800 border border-slate-700 rounded-md px-3 h-9 text-sm text-slate-200"
          />
        ) : (
          <p className="text-sm text-slate-400">{script.description || 'No description'}</p>
        )}
        <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-xs text-slate-500">
          <span>Timeout: {script.timeout_secs}s</span>
          <span>Runs: {script.run_count ?? 0}</span>
          <span>Last run: {formatTime(script.last_run)}</span>
          <span>Created: {formatDateTime(script.created_at)}</span>
          <span>Updated: {formatDateTime(script.updated_at)}</span>
          {script.tags && script.tags.length > 0 && (
            <span className="inline-flex items-center gap-1">
              <Tag className="h-3 w-3" />
              {script.tags.join(', ')}
            </span>
          )}
        </div>
      </div>

      {/* Code */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/50 overflow-hidden">
        <div className="flex items-center gap-2 px-4 h-11 border-b border-slate-800 text-sm text-slate-400">
          {editing ? <Edit3 className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          {editing ? 'Editing' : 'Source'}
        </div>
        {editing ? (
          <textarea
            value={draft.content}
            onChange={(e) => setDraft({ ...draft, content: e.target.value })}
            spellCheck={false}
            className="w-full h-80 bg-slate-950 text-slate-200 font-mono text-sm p-4 resize-y outline-none"
          />
        ) : (
          <pre className="p-4 overflow-auto h-80 text-sm font-mono text-slate-200 bg-slate-950">
            {script.content}
          </pre>
        )}
      </div>

      {/* Run history */}
      <RunHistoryTable runs={runs} />

      {showRunModal && (
        <RunNowModal
          agents={agents}
          selected={selectedAgents}
          onToggle={(id) =>
            setSelectedAgents((prev) => {
              const next = new Set(prev);
              if (next.has(id)) {
                next.delete(id);
              } else {
                next.add(id);
              }
              return next;
            })
          }
          onClose={() => setShowRunModal(false)}
          onRun={onRun}
          running={false}
        />
      )}
    </div>
  );
}
