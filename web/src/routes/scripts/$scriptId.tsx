// Script detail — view, edit, and run a script.
//
// Sections:
//   • Script info card: name, description, runtime, timeout, tags, timestamps
//   • Code viewer (read-only, syntax-highlighted) / toggle to edit mode
//   • Run history table
//   • "Run Now" action — select target agent(s) and execute
//
// Edit mode uses the same lightweight CodeEditor as the create page and
// PATCHes the script on save.

import { createFileRoute, useNavigate, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
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
  X,
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

export const Route = createFileRoute('/scripts/$scriptId')({
  component: ScriptDetailPage,
});

const RUNTIME_META: Record<
  ScriptRuntime,
  { label: string; icon: typeof Terminal; classes: string }
> = {
  bash: {
    label: 'Bash',
    icon: Terminal,
    classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20',
  },
  powershell: {
    label: 'PowerShell',
    icon: Terminal,
    classes: 'bg-sky-500/10 text-sky-300 border-sky-500/20',
  },
  python: {
    label: 'Python',
    icon: Code2,
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
  },
  node: {
    label: 'Node',
    icon: Braces,
    classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20',
  },
};

const STATUS_META: Record<
  ScriptRunStatus,
  { label: string; classes: string; icon: typeof CircleCheck }
> = {
  pending: {
    label: 'Pending',
    classes: 'bg-slate-500/10 text-slate-300 border-slate-500/20',
    icon: CircleDashed,
  },
  in_progress: {
    label: 'Running',
    classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20',
    icon: CirclePlay,
  },
  completed: {
    label: 'Success',
    classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20',
    icon: CircleCheck,
  },
  failed: {
    label: 'Failed',
    classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20',
    icon: CircleX,
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-slate-500/10 text-slate-400 border-slate-500/20',
    icon: CircleDashed,
  },
  timeout: {
    label: 'Timeout',
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
    icon: CircleAlert,
  },
};

function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

function formatDuration(ms?: number): string {
  if (ms === undefined || ms === null) return '—';
  if (ms < 1000) return `${ms}ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  return `${m}m ${rs}s`;
}

// Simple, deterministic token-based highlighter for the read-only viewer.
// It doesn't aim to match a full lexer — it tags comments, strings,
// keywords, and numbers, which is more than enough for at-a-glance review.
function highlight(code: string, language: ScriptRuntime): Array<{ text: string; cls?: string }> {
  type Tok = { text: string; cls?: string };

  const KEYWORDS: Record<ScriptRuntime, string[]> = {
    bash: [
      'if', 'then', 'else', 'elif', 'fi', 'case', 'esac', 'for', 'while',
      'do', 'done', 'function', 'return', 'exit', 'echo', 'export', 'local',
      'set', 'unset', 'source', 'true', 'false',
    ],
    powershell: [
      'function', 'param', 'if', 'else', 'elseif', 'switch', 'foreach', 'for',
      'while', 'do', 'until', 'return', 'throw', 'try', 'catch', 'finally',
      'begin', 'process', 'end', 'in',
    ],
    python: [
      'def', 'class', 'if', 'elif', 'else', 'for', 'while', 'in', 'is', 'not',
      'and', 'or', 'return', 'yield', 'import', 'from', 'as', 'try', 'except',
      'finally', 'raise', 'with', 'lambda', 'pass', 'break', 'continue',
      'True', 'False', 'None',
    ],
    node: [
      'const', 'let', 'var', 'function', 'return', 'if', 'else', 'for',
      'while', 'do', 'switch', 'case', 'break', 'continue', 'class', 'extends',
      'new', 'this', 'super', 'import', 'export', 'from', 'as', 'async',
      'await', 'try', 'catch', 'finally', 'throw', 'typeof', 'instanceof',
      'true', 'false', 'null', 'undefined',
    ],
  };

  const keywords = new Set(KEYWORDS[language]);

  const isLineComment =
    language === 'bash' || language === 'python' || language === 'node' || language === 'powershell';

  // Tokenize line by line.
  const lines = code.split('\n');
  const tokens: Tok[] = [];
  for (let li = 0; li < lines.length; li++) {
    const line = lines[li];
    let i = 0;
    while (i < line.length) {
      const ch = line[i];

      // Line comment
      if (isLineComment && (ch === '#' || (language === 'node' && ch === '/' && line[i + 1] === '/'))) {
        tokens.push({ text: line.substring(i), cls: 'text-slate-500 italic' });
        i = line.length;
        break;
      }

      // Strings (single and double quote, very rough)
      if (ch === '"' || ch === "'" || (language === 'node' && ch === '`' )) {
        const quote = ch;
        let j = i + 1;
        while (j < line.length && line[j] !== quote) {
          if (line[j] === '\\') j += 1;
          j += 1;
        }
        j = Math.min(j + 1, line.length);
        tokens.push({ text: line.substring(i, j), cls: 'text-emerald-300' });
        i = j;
        continue;
      }

      // Numbers
      if (/\d/.test(ch) && (i === 0 || /[\s,(\[]/.test(line[i - 1]))) {
        let j = i;
        while (j < line.length && /[\d._a-zA-Z]/.test(line[j])) j += 1;
        tokens.push({ text: line.substring(i, j), cls: 'text-amber-300' });
        i = j;
        continue;
      }

      // Words → keywords
      if (/[A-Za-z_]/.test(ch)) {
        let j = i;
        while (j < line.length && /[A-Za-z0-9_]/.test(line[j])) j += 1;
        const word = line.substring(i, j);
        if (keywords.has(word)) {
          tokens.push({ text: word, cls: 'text-indigo-300' });
        } else if (/^[A-Z_][A-Z0-9_]*$/.test(word) && word.length > 1) {
          tokens.push({ text: word, cls: 'text-rose-300' });
        } else {
          tokens.push({ text: word });
        }
        i = j;
        continue;
      }

      // Default: single char
      tokens.push({ text: ch });
      i += 1;
    }
    if (li < lines.length - 1) tokens.push({ text: '\n' });
  }
  return tokens;
}

function ScriptDetailPage() {
  const { scriptId } = Route.useParams();
  const navigate = useNavigate();
  const { fetchScript, updateScript, deleteScript, runScript, fetchRuns } = useScripts();
  const { agents } = useAgents();

  const [script, setScript] = useState<Script | null>(null);
  const [runs, setRuns] = useState<ScriptRun[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [editRuntime, setEditRuntime] = useState<ScriptRuntime>('bash');
  const [editTimeout, setEditTimeout] = useState(60);
  const [editTagsInput, setEditTagsInput] = useState('');
  const [editContent, setEditContent] = useState('');
  const [savingEdit, setSavingEdit] = useState(false);

  const [showRunNow, setShowRunNow] = useState(false);
  const [targetAgentIds, setTargetAgentIds] = useState<Set<string>>(new Set());
  const [runningNow, setRunningNow] = useState(false);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const reload = useCallback(async () => {
    try {
      const [s, r] = await Promise.all([
        fetchScript(scriptId),
        fetchRuns(scriptId, 50).catch(() => [] as ScriptRun[]),
      ]);
      setScript(s);
      setRuns(r);
      setEditName(s.name);
      setEditDescription(s.description ?? '');
      setEditRuntime(s.runtime);
      setEditTimeout(s.timeout_secs);
      setEditTagsInput((s.tags ?? []).join(', '));
      setEditContent(s.content);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIsLoading(false);
    }
  }, [scriptId, fetchScript, fetchRuns]);

  useEffect(() => {
    setIsLoading(true);
    void reload();
  }, [reload]);

  // -----------------------------------------------------------------------
  // Edit / Save
  // -----------------------------------------------------------------------

  const onSaveEdit = async () => {
    if (!script) return;
    setSavingEdit(true);
    try {
      const tags = editTagsInput
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean);
      const updated = await updateScript(script.id, {
        name: editName.trim(),
        description: editDescription.trim() || undefined,
        runtime: editRuntime,
        content: editContent,
        timeout_secs: editTimeout,
        tags: tags.length > 0 ? tags : undefined,
      });
      setScript(updated);
      setIsEditing(false);
      toast.success('Script updated');
    } catch (e) {
      toast.error(`Update failed: ${(e as Error).message}`);
    } finally {
      setSavingEdit(false);
    }
  };

  const onDelete = async () => {
    if (!script) return;
    if (!confirm(`Delete script "${script.name}"? This cannot be undone.`)) return;
    try {
      await deleteScript(script.id);
      toast.success(`Deleted "${script.name}"`);
      void navigate({ to: '/scripts' });
    } catch (e) {
      toast.error(`Delete failed: ${(e as Error).message}`);
    }
  };

  // -----------------------------------------------------------------------
  // Run Now
  // -----------------------------------------------------------------------

  const toggleAgent = (id: string) => {
    setTargetAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const onRunNow = async () => {
    if (!script) return;
    if (targetAgentIds.size === 0) {
      toast.error('Select at least one agent');
      return;
    }
    setRunningNow(true);
    try {
      const run = await runScript(script.id, Array.from(targetAgentIds));
      toast.success(`Run started on ${targetAgentIds.size} agent(s)`);
      setShowRunNow(false);
      setTargetAgentIds(new Set());
      // Reload runs list and navigate to the first run.
      const r = await fetchRuns(script.id, 50).catch(() => [] as ScriptRun[]);
      setRuns(r);
      void navigate({
        to: '/scripts/$scriptId/runs/$runId',
        params: { scriptId: script.id, runId: run.id },
      });
    } catch (e) {
      toast.error(`Run failed: ${(e as Error).message}`);
    } finally {
      setRunningNow(false);
    }
  };

  // -----------------------------------------------------------------------
  // Render
  // -----------------------------------------------------------------------

  if (isLoading && !script) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
        <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
        Loading script…
      </div>
    );
  }

  if (!script) {
    return (
      <div className="space-y-4">
        <Link
          to="/scripts"
          className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"
        >
          <ArrowLeft className="h-4 w-4" /> Back to scripts
        </Link>
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
          Script not found.
        </div>
      </div>
    );
  }

  const runtimeMeta = RUNTIME_META[script.runtime];
  const RuntimeIcon = runtimeMeta.icon;
  const editTags = editTagsInput
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/scripts"
            className="p-2 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <RuntimeIcon className="h-4 w-4 text-slate-300" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold text-slate-100">{script.name}</h1>
              <span
                className={
                  'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-xs ' + runtimeMeta.classes
                }
              >
                <RuntimeIcon className="h-3 w-3" />
                {runtimeMeta.label}
              </span>
            </div>
            <p className="text-slate-400 text-sm mt-0.5">
              {script.description ?? (
                <span className="italic text-slate-500">No description</span>
              )}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          <button
            type="button"
            onClick={() => {
              if (isEditing) {
                // Cancel — reset fields
                setEditName(script.name);
                setEditDescription(script.description ?? '');
                setEditRuntime(script.runtime);
                setEditTimeout(script.timeout_secs);
                setEditTagsInput((script.tags ?? []).join(', '));
                setEditContent(script.content);
                setIsEditing(false);
              } else {
                setIsEditing(true);
              }
            }}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-slate-200 transition-colors"
          >
            {isEditing ? <Eye className="h-4 w-4" /> : <Edit3 className="h-4 w-4" />}
            <span>{isEditing ? 'View' : 'Edit'}</span>
          </button>
          {isEditing && (
            <button
              type="button"
              onClick={() => void onSaveEdit()}
              disabled={savingEdit}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white disabled:opacity-50 transition-colors"
            >
              {savingEdit ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              <span>{savingEdit ? 'Saving…' : 'Save'}</span>
            </button>
          )}
          <button
            type="button"
            onClick={() => setShowRunNow(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white transition-colors"
          >
            <Play className="h-4 w-4" />
            <span>Run Now</span>
          </button>
          <button
            type="button"
            onClick={() => void onDelete()}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-rose-500/30 bg-rose-500/10 text-rose-300 hover:bg-rose-500/20 text-sm transition-colors"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-300">
          {error}
        </div>
      )}

      {/* Info card */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5">
        {isEditing ? (
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-slate-400 mb-1">Name</label>
              <input
                type="text"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
              />
            </div>
            <div>
              <label className="block text-xs text-slate-400 mb-1">Description</label>
              <textarea
                value={editDescription}
                onChange={(e) => setEditDescription(e.target.value)}
                rows={2}
                className="w-full px-3 py-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40 resize-none"
              />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div>
                <label className="block text-xs text-slate-400 mb-1">Runtime</label>
                <select
                  value={editRuntime}
                  onChange={(e) => setEditRuntime(e.target.value as ScriptRuntime)}
                  className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                >
                  {Object.entries(RUNTIME_META).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-slate-400 mb-1">Timeout (s)</label>
                <input
                  type="number"
                  min={5}
                  max={3600}
                  value={editTimeout}
                  onChange={(e) => setEditTimeout(Math.max(5, Number(e.target.value) || 60))}
                  className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                />
              </div>
              <div>
                <label className="block text-xs text-slate-400 mb-1">Tags (comma-sep)</label>
                <input
                  type="text"
                  value={editTagsInput}
                  onChange={(e) => setEditTagsInput(e.target.value)}
                  className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                />
              </div>
            </div>
            {editTags.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {editTags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-slate-800 border border-slate-700 text-slate-300"
                  >
                    <Tag className="h-3 w-3" /> {t}
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Name</p>
              <p className="text-sm text-slate-100 mt-1">{script.name}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Runtime</p>
              <p className="text-sm text-slate-100 mt-1">{runtimeMeta.label}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Timeout</p>
              <p className="text-sm text-slate-100 mt-1">{script.timeout_secs}s</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Total Runs</p>
              <p className="text-sm text-slate-100 mt-1 tabular-nums">
                {script.run_count ?? runs.length}
              </p>
            </div>
            {script.description && (
              <div className="sm:col-span-2 lg:col-span-4">
                <p className="text-xs uppercase tracking-wider text-slate-500">Description</p>
                <p className="text-sm text-slate-200 mt-1">{script.description}</p>
              </div>
            )}
            {script.tags && script.tags.length > 0 && (
              <div className="sm:col-span-2 lg:col-span-4">
                <p className="text-xs uppercase tracking-wider text-slate-500">Tags</p>
                <div className="flex flex-wrap gap-1 mt-1">
                  {script.tags.map((t) => (
                    <span
                      key={t}
                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-slate-800 border border-slate-700 text-slate-300"
                    >
                      <Tag className="h-3 w-3" /> {t}
                    </span>
                  ))}
                </div>
              </div>
            )}
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Created</p>
              <p className="text-sm text-slate-100 mt-1">{formatDateTime(script.created_at)}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Updated</p>
              <p className="text-sm text-slate-100 mt-1">{formatDateTime(script.updated_at)}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Last Run</p>
              <p className="text-sm text-slate-100 mt-1">{formatTime(script.last_run, now)}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-slate-500">Script ID</p>
              <p className="text-xs text-slate-400 mt-1 font-mono">{script.id}</p>
            </div>
          </div>
        )}
      </div>

      {/* Code viewer / editor */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden">
        <div className="px-5 py-3 border-b border-slate-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileCode2 className="h-4 w-4 text-slate-400" />
            <h2 className="text-sm font-semibold text-slate-100">Code</h2>
            <span className="text-xs text-slate-500">· {runtimeMeta.label}</span>
          </div>
          {!isEditing && (
            <span className="text-xs text-slate-500 font-mono">
              {script.content.split('\n').length} lines
            </span>
          )}
        </div>
        {isEditing ? (
          <CodeEditor
            value={editContent}
            onChange={setEditContent}
            language={editRuntime}
            rows={20}
          />
        ) : (
          <CodeViewer code={script.content} language={script.runtime} />
        )}
      </div>

      {/* Run history */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100">Run History</h2>
          <span className="text-xs text-slate-500">
            {runs.length} run{runs.length === 1 ? '' : 's'}
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-4 py-3">Run ID</th>
                <th className="px-4 py-3">Agent</th>
                <th className="px-4 py-3 w-40">Started</th>
                <th className="px-4 py-3 w-40">Finished</th>
                <th className="px-4 py-3 w-24 text-right">Duration</th>
                <th className="px-4 py-3 w-24">Status</th>
                <th className="px-4 py-3 w-20 text-right">Exit</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {runs.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                    No runs yet. Click "Run Now" to execute this script.
                  </td>
                </tr>
              ) : (
                runs.map((r) => {
                  const meta = STATUS_META[r.status] ?? STATUS_META.pending;
                  return (
                    <tr
                      key={r.id}
                      onClick={() =>
                        void navigate({
                          to: '/scripts/$scriptId/runs/$runId',
                          params: { scriptId: script.id, runId: r.id },
                        })
                      }
                      className="hover:bg-slate-800/40 cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3 font-mono text-xs text-slate-300">
                        {r.id.slice(0, 8)}
                      </td>
                      <td className="px-4 py-3 text-slate-200">
                        {r.hostname ?? r.agent_id}
                      </td>
                      <td className="px-4 py-3 text-slate-400">
                        {formatTime(r.started_at, now)}
                      </td>
                      <td className="px-4 py-3 text-slate-400">
                        {formatTime(r.finished_at, now)}
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-300">
                        {formatDuration(r.duration_ms)}
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={
                            'inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-xs ' +
                            meta.classes
                          }
                        >
                          <meta.icon className="h-3 w-3" />
                          {meta.label}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-300">
                        {r.exit_code !== undefined && r.exit_code !== null ? (
                          <span
                            className={
                              r.exit_code === 0 ? 'text-emerald-400' : 'text-rose-400'
                            }
                          >
                            {r.exit_code}
                          </span>
                        ) : (
                          '—'
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Run Now modal */}
      {showRunNow && (
        <RunNowModal
          agents={agents}
          selected={targetAgentIds}
          onToggle={toggleAgent}
          onClose={() => setShowRunNow(false)}
          onRun={() => void onRunNow()}
          running={runningNow}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Code viewer (read-only, syntax-highlighted)
// ---------------------------------------------------------------------------

function CodeViewer({ code, language }: { code: string; language: ScriptRuntime }) {
  const tokens = useMemo(() => highlight(code, language), [code, language]);
  return (
    <div className="flex bg-slate-950 font-mono text-sm min-h-[20rem] max-h-[40rem] overflow-auto">
      <div className="select-none text-right text-slate-600 bg-slate-950 border-r border-slate-800 py-3 px-2 shrink-0" style={{ width: '3.5rem' }}>
        {Array.from({ length: code.split('\n').length }, (_, i) => i + 1).map((n) => (
          <div key={n} className="leading-6 text-xs">
            {n}
          </div>
        ))}
      </div>
      <pre className="flex-1 py-3 px-3 text-slate-200 leading-6 whitespace-pre overflow-auto min-w-0">
        {tokens.map((t, i) => (
          <span key={i} className={t.cls}>
            {t.text}
          </span>
        ))}
      </pre>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Code editor (lightweight, for edit mode)
// ---------------------------------------------------------------------------

function CodeEditor({
  value,
  onChange,
  language,
  rows = 20,
}: {
  value: string;
  onChange: (next: string) => void;
  language: ScriptRuntime;
  rows?: number;
}) {
  const gutterRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const lineCount = useMemo(() => value.split('\n').length, [value]);
  const lineNumbers = useMemo(
    () => Array.from({ length: Math.max(lineCount, rows) }, (_, i) => i + 1),
    [lineCount, rows]
  );

  const onScroll = useCallback(() => {
    if (gutterRef.current && textareaRef.current) {
      gutterRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  }, []);

  return (
    <div className="flex bg-slate-950 font-mono text-sm min-h-[20rem] max-h-[40rem]">
      <div
        ref={gutterRef}
        className="select-none text-right text-slate-600 bg-slate-950 border-r border-slate-800 py-3 px-2 overflow-hidden shrink-0"
        style={{ width: '3.5rem' }}
        aria-hidden
      >
        {lineNumbers.map((n) => (
          <div key={n} className="leading-6 text-xs">
            {n}
          </div>
        ))}
      </div>
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onScroll={onScroll}
        spellCheck={false}
        rows={rows}
        data-language={language}
        className="flex-1 bg-slate-950 text-slate-100 py-3 px-3 resize-none outline-none leading-6 whitespace-pre overflow-auto min-h-0"
        style={{ tabSize: 2 }}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Run Now modal
// ---------------------------------------------------------------------------

function RunNowModal({
  agents,
  selected,
  onToggle,
  onClose,
  onRun,
  running,
}: {
  agents: ReturnType<typeof useAgents>['agents'];
  selected: Set<string>;
  onToggle: (id: string) => void;
  onClose: () => void;
  onRun: () => void;
  running: boolean;
}) {
  const [query, setQuery] = useState('');
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return agents;
    return agents.filter(
      (a) =>
        a.hostname.toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q) ||
        a.os?.toLowerCase().includes(q)
    );
  }, [agents, query]);

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md rounded-lg border border-slate-800 bg-slate-900 shadow-xl">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100">Run Now</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-4 space-y-3">
          <div className="relative">
            <Globe className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search agents…"
              className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
          </div>
          <ul className="max-h-80 overflow-y-auto divide-y divide-slate-800 rounded-md border border-slate-800">
            {filtered.length === 0 ? (
              <li className="px-3 py-6 text-center text-slate-500 text-sm">
                No agents available.
              </li>
            ) : (
              filtered.map((a) => {
                const isSelected = selected.has(a.id);
                return (
                  <li
                    key={a.id}
                    onClick={() => onToggle(a.id)}
                    className={
                      'px-3 py-2 flex items-center justify-between cursor-pointer transition-colors ' +
                      (isSelected ? 'bg-indigo-500/10' : 'hover:bg-slate-800/40')
                    }
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => onToggle(a.id)}
                        className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500/40"
                      />
                      <div className="min-w-0">
                        <p className="text-sm text-slate-200 truncate">{a.hostname || a.id}</p>
                        <p className="text-xs text-slate-500 truncate">
                          {a.os} · {a.status}
                        </p>
                      </div>
                    </div>
                  </li>
                );
              })
            )}
          </ul>
          <p className="text-xs text-slate-500">
            {selected.size} agent{selected.size === 1 ? '' : 's'} selected
          </p>
        </div>
        <div className="px-5 py-3 border-t border-slate-800 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-slate-200 hover:bg-slate-700 transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={running || selected.size === 0}
            onClick={onRun}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white disabled:opacity-50 transition-colors"
          >
            {running ? <Loader2 className="h-4 w-4 animate-spin" /> : <CirclePlay className="h-4 w-4" />}
            <span>{running ? 'Starting…' : 'Run'}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
