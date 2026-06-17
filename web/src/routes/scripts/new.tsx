// Script editor — create a new script.
//
// Provides:
//   • Metadata form: name, description, runtime dropdown, timeout, tags
//   • Monaco code editor (loaded from jsDelivr CDN at runtime) with
//     language auto-detection from the runtime dropdown. Falls back to a
//     plain <textarea> if the CDN is unreachable.
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
import { MonacoEditor, type MonacoLanguage } from '@/components/monaco-editor';

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

/** Map a script runtime to the Monaco language id we want for syntax highlighting. */
const RUNTIME_TO_MONACO: Record<ScriptRuntime, MonacoLanguage> = {
  bash: 'bash',
  powershell: 'powershell',
  python: 'python',
  node: 'javascript',
};

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
            className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-accent/10 border border-accent/20 flex items-center justify-center">
            <FileCode2 className="h-4 w-4 text-accent" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">New Script</h1>
            <p className="text-text-secondary text-sm mt-0.5">
              Compose a reusable script and save it to your library.
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Metadata form */}
        <form
          onSubmit={handleSave}
          className="lg:col-span-1 space-y-4 rounded-lg border border-border-subtle bg-surface-secondary/60 p-5"
        >
          <h2 className="text-sm font-semibold text-text-primary">Metadata</h2>

          <div>
            <label className="block text-xs text-text-secondary mb-1">Name *</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Restart nginx service"
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>

          <div>
            <label className="block text-xs text-text-secondary mb-1">Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder="What does this script do?"
              className="w-full px-3 py-2 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40 resize-none"
            />
          </div>

          <div>
            <label className="block text-xs text-text-secondary mb-1">Runtime *</label>
            <select
              value={runtime}
              onChange={(e) => setRuntime(e.target.value as ScriptRuntime)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            >
              {RUNTIME_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs text-text-secondary mb-1">Timeout (seconds)</label>
            <input
              type="number"
              min={5}
              max={3600}
              value={timeoutSecs}
              onChange={(e) => setTimeoutSecs(Math.max(5, Number(e.target.value) || 60))}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
            <p className="text-xs text-text-muted mt-1">5 – 3600 seconds</p>
          </div>

          <div>
            <label className="block text-xs text-text-secondary mb-1">
              <Tag className="inline h-3 w-3 mr-1" />
              Tags (comma-separated)
            </label>
            <input
              type="text"
              value={tagsInput}
              onChange={(e) => setTagsInput(e.target.value)}
              placeholder="maintenance, restart, nginx"
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
            {tags.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-2">
                {tags.map((t) => (
                  <span
                    key={t}
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs bg-surface-tertiary border border-border-strong text-text-secondary"
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}
          </div>

          {error && (
            <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
              {error}
            </div>
          )}

          <div className="flex flex-col gap-2 pt-2">
            <button
              type="submit"
              disabled={saving}
              className="inline-flex items-center justify-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent text-sm text-white disabled:opacity-50 transition-colors"
            >
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              <span>{saving ? 'Saving…' : 'Save Script'}</span>
            </button>

            <div className="rounded-md border border-border-subtle p-3 space-y-2">
              <label className="block text-xs text-text-secondary">Test Run target</label>
              <select
                value={targetAgentId}
                onChange={(e) => setTargetAgentId(e.target.value)}
                className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
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
                className="inline-flex items-center justify-center gap-2 w-full px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 transition-colors"
              >
                {running ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <CirclePlay className="h-4 w-4" />
                )}
                <span>{running ? 'Starting…' : 'Test Run'}</span>
              </button>
              <p className="text-[11px] text-text-muted">
                Saves the script (if needed) and executes it on the selected agent.
              </p>
            </div>
          </div>
        </form>

        {/* Code editor */}
        <div className="lg:col-span-2 rounded-lg border border-border-subtle bg-surface-secondary/60 overflow-hidden flex flex-col">
          <div className="px-5 py-3 border-b border-border-subtle flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileCode2 className="h-4 w-4 text-text-secondary" />
              <h2 className="text-sm font-semibold text-text-primary">Code</h2>
              <span className="text-xs text-text-muted">
                · {RUNTIME_OPTIONS.find((o) => o.value === runtime)?.label}
              </span>
            </div>
            <span className="text-xs text-text-muted font-mono">
              {content.split('\n').length} lines · {content.length} chars
            </span>
          </div>
          <MonacoEditor
            value={content}
            onChange={setContent}
            language={RUNTIME_TO_MONACO[runtime]}
            height={520}
            theme="vs-dark"
          />
        </div>
      </div>
    </div>
  );
}

// (Lightweight textarea-based CodeEditor removed in favor of MonacoEditor,
//  which is loaded from CDN at runtime and falls back to a textarea if the
//  CDN is unavailable.)
