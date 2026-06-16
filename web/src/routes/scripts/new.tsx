// Script editor — create a new script.
//
// Provides:
//   • Metadata form: name, description, runtime dropdown, timeout, tags
//   • Lightweight code editor: textarea + line numbers, monospace font
//     (Monaco is heavy and not part of the bundle; we use a simple
//     textarea with a paired line-number gutter for the same UX shape.)
//   • Runtime-specific syntax mode stored in component state
//   • Save → POST /api/v1/scripts
//   • Test Run → selects a target agent and POSTs /api/v1/scripts/{id}/run

import { createFileRoute, useNavigate, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeft,
  FileCode2,
  Save,
  Play,
  Loader2,
  X,
  CirclePlay,
  Terminal,
  Code2,
  Braces,
  Tag,
} from 'lucide-react';
import { toast } from 'sonner';
import { useScripts, type ScriptRuntime } from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';

export const Route = createFileRoute('/scripts/new')({
  component: NewScriptPage,
});

const RUNTIME_OPTIONS: { value: ScriptRuntime; label: string; icon: typeof Terminal; placeholder: string }[] = [
  {
    value: 'bash',
    label: 'Bash',
    icon: Terminal,
    placeholder: '#!/usr/bin/env bash\nset -euo pipefail\n\necho "Hello, world!"\n',
  },
  {
    value: 'powershell',
    label: 'PowerShell',
    icon: Terminal,
    placeholder: '# PowerShell script\n$ErrorActionPreference = "Stop"\n\nWrite-Host "Hello, world!"\n',
  },
  {
    value: 'python',
    label: 'Python',
    icon: Code2,
    placeholder: '#!/usr/bin/env python3\nimport sys\n\ndef main():\n    print("Hello, world!")\n\nif __name__ == "__main__":\n    main()\n',
  },
  {
    value: 'node',
    label: 'Node',
    icon: Braces,
    placeholder: '// Node.js script\nconst main = async () => {\n  console.log("Hello, world!");\n};\n\nmain().catch(console.error);\n',
  },
];

function defaultTemplate(rt: ScriptRuntime): string {
  return RUNTIME_OPTIONS.find((o) => o.value === rt)?.placeholder ?? '';
}

function NewScriptPage() {
  const navigate = useNavigate();
  const { createScript, runScript } = useScripts();
  const { agents } = useAgents();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [runtime, setRuntime] = useState<ScriptRuntime>('bash');
  const [timeoutSecs, setTimeoutSecs] = useState(60);
  const [tagsInput, setTagsInput] = useState('');
  const [content, setContent] = useState<string>(defaultTemplate('bash'));

  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createdScriptId, setCreatedScriptId] = useState<string | null>(null);
  const [targetAgentId, setTargetAgentId] = useState<string>('');

  const tags = useMemo(
    () =>
      tagsInput
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean),
    [tagsInput]
  );

  // Update the editor's content template when the runtime changes, but only
  // if the user hasn't typed anything yet. This avoids clobbering edits.
  const initialContentRef = useRef(true);
  useEffect(() => {
    if (initialContentRef.current) {
      initialContentRef.current = false;
      return;
    }
    setContent(defaultTemplate(runtime));
  }, [runtime]);

  // Pick a sensible default agent (first online).
  useEffect(() => {
    if (targetAgentId) return;
    const first = agents.find((a) => a.status === 'online') ?? agents[0];
    if (first) setTargetAgentId(first.id);
  }, [agents, targetAgentId]);

  const validate = useCallback((): string | null => {
    if (!name.trim()) return 'Name is required';
    if (!content.trim()) return 'Script content cannot be empty';
    if (timeoutSecs < 5) return 'Timeout must be at least 5 seconds';
    if (timeoutSecs > 3600) return 'Timeout cannot exceed 3600 seconds';
    return null;
  }, [name, content, timeoutSecs]);

  const handleSave = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    setError(null);
    const err = validate();
    if (err) {
      setError(err);
      return;
    }
    setSaving(true);
    try {
      const s = await createScript({
        name: name.trim(),
        description: description.trim() || undefined,
        runtime,
        content,
        timeout_secs: timeoutSecs,
        tags: tags.length > 0 ? tags : undefined,
      });
      toast.success(`Created "${s.name}"`);
      setCreatedScriptId(s.id);
      void navigate({ to: '/scripts/$scriptId', params: { scriptId: s.id } });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const handleTestRun = async () => {
    setError(null);
    if (!targetAgentId) {
      setError('Please select a target agent for the test run');
      return;
    }
    setRunning(true);
    try {
      // If the script hasn't been saved yet, save it first.
      let scriptId = createdScriptId;
      if (!scriptId) {
        const err = validate();
        if (err) {
          setError(err);
          setRunning(false);
          return;
        }
        const s = await createScript({
          name: name.trim(),
          description: description.trim() || undefined,
          runtime,
          content,
          timeout_secs: timeoutSecs,
          tags: tags.length > 0 ? tags : undefined,
        });
        scriptId = s.id;
        setCreatedScriptId(s.id);
      }
      const run = await runScript(scriptId, [targetAgentId]);
      toast.success(`Test run started — #${run.id.slice(0, 8)}`);
      void navigate({
        to: '/scripts/$scriptId/runs/$runId',
        params: { scriptId, runId: run.id },
      });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/scripts"
            className="p-2 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center">
            <FileCode2 className="h-4 w-4 text-indigo-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">New Script</h1>
            <p className="text-slate-400 text-sm mt-0.5">
              Compose a reusable script and save it to your library.
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Metadata form */}
        <form
          onSubmit={handleSave}
          className="lg:col-span-1 space-y-4 rounded-lg border border-slate-800 bg-slate-900/60 p-5"
        >
          <h2 className="text-sm font-semibold text-slate-100">Metadata</h2>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Name *</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Restart nginx service"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder="What does this script do?"
              className="w-full px-3 py-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40 resize-none"
            />
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Runtime *</label>
            <select
              value={runtime}
              onChange={(e) => setRuntime(e.target.value as ScriptRuntime)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            >
              {RUNTIME_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Timeout (seconds)</label>
            <input
              type="number"
              min={5}
              max={3600}
              value={timeoutSecs}
              onChange={(e) => setTimeoutSecs(Math.max(5, Number(e.target.value) || 60))}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
            <p className="text-xs text-slate-500 mt-1">5 – 3600 seconds</p>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">
              <Tag className="inline h-3 w-3 mr-1" />
              Tags (comma-separated)
            </label>
            <input
              type="text"
              value={tagsInput}
              onChange={(e) => setTagsInput(e.target.value)}
              placeholder="maintenance, restart, nginx"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
            {tags.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-2">
                {tags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-slate-800 border border-slate-700 text-slate-300"
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
          </div>

          {error && (
            <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
              {error}
            </div>
          )}

          <div className="flex flex-col gap-2 pt-2">
            <button
              type="submit"
              disabled={saving}
              className="inline-flex items-center justify-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white disabled:opacity-50 transition-colors"
            >
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              <span>{saving ? 'Saving…' : 'Save Script'}</span>
            </button>

            <div className="rounded-md border border-slate-800 p-3 space-y-2">
              <label className="block text-xs text-slate-400">Test Run target</label>
              <select
                value={targetAgentId}
                onChange={(e) => setTargetAgentId(e.target.value)}
                className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
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
                onClick={() => void handleTestRun()}
                className="inline-flex items-center justify-center gap-2 w-full px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-slate-200 disabled:opacity-50 transition-colors"
              >
                {running ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <CirclePlay className="h-4 w-4" />
                )}
                <span>{running ? 'Starting…' : 'Test Run'}</span>
              </button>
              <p className="text-[11px] text-slate-500">
                Saves the script (if needed) and executes it on the selected agent.
              </p>
            </div>
          </div>
        </form>

        {/* Code editor */}
        <div className="lg:col-span-2 rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden flex flex-col">
          <div className="px-5 py-3 border-b border-slate-800 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileCode2 className="h-4 w-4 text-slate-400" />
              <h2 className="text-sm font-semibold text-slate-100">Code</h2>
              <span className="text-xs text-slate-500">
                · {RUNTIME_OPTIONS.find((o) => o.value === runtime)?.label}
              </span>
            </div>
            <span className="text-xs text-slate-500 font-mono">
              {content.split('\n').length} lines · {content.length} chars
            </span>
          </div>
          <CodeEditor
            value={content}
            onChange={setContent}
            language={runtime}
            rows={24}
          />
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Lightweight code editor
// ---------------------------------------------------------------------------
//
// A paired line-number gutter + textarea. This avoids pulling Monaco into
// the bundle (which is hundreds of KBs and not a dependency today). The
// gutter and textarea scroll together via a shared scroll handler.

interface CodeEditorProps {
  value: string;
  onChange: (next: string) => void;
  language: ScriptRuntime;
  rows?: number;
}

function CodeEditor({ value, onChange, language, rows = 20 }: CodeEditorProps) {
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

  // Tab key inserts spaces rather than changing focus.
  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Tab') {
        e.preventDefault();
        const el = e.currentTarget;
        const start = el.selectionStart;
        const end = el.selectionEnd;
        const next = value.substring(0, start) + '  ' + value.substring(end);
        onChange(next);
        requestAnimationFrame(() => {
          el.selectionStart = el.selectionEnd = start + 2;
        });
      }
    },
    [value, onChange]
  );

  return (
    <div className="flex-1 flex bg-slate-950 font-mono text-sm min-h-0">
      {/* Line-number gutter */}
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

      {/* Textarea */}
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onScroll={onScroll}
        onKeyDown={onKeyDown}
        spellCheck={false}
        rows={rows}
        data-language={language}
        className="flex-1 bg-slate-950 text-slate-100 py-3 px-3 resize-none outline-none leading-6 whitespace-pre overflow-auto min-h-0"
        style={{ tabSize: 2 }}
        placeholder={`Write your ${language} script here…`}
      />
    </div>
  );
}
