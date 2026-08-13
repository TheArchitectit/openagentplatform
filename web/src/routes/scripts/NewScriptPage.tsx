// NewScriptPage — the page component for creating a new script.
//
// Extracted from routes/scripts/new.tsx (which keeps the TanStack Route
// export). Contains all form state, save / test-run handlers, and the
// Monaco editor UI. The metadata form lives in NewScriptPage.metadata-form.tsx.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeft,
  FileCode2,
} from 'lucide-react';
import { useNavigate, Link } from '@tanstack/react-router';
import { toast } from 'sonner';
import { useScripts, type ScriptRuntime } from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';
import { MonacoEditor } from '@/components/monaco-editor';
import { RUNTIME_OPTIONS, defaultTemplate, RUNTIME_TO_MONACO } from './script-runtime-options';
import { MetadataFormSection } from './NewScriptPage.metadata-form';

export function NewScriptPage() {
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
            className="p-2 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-blue-600/10 border border-blue-500/20 flex items-center justify-center">
            <FileCode2 className="h-4 w-4 text-blue-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">New Script</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Compose a reusable script and save it to your library.
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        <MetadataFormSection
          name={name}
          description={description}
          runtime={runtime}
          timeoutSecs={timeoutSecs}
          tagsInput={tagsInput}
          tags={tags}
          error={error}
          saving={saving}
          running={running}
          agents={agents}
          targetAgentId={targetAgentId}
          onName={setName}
          onDescription={setDescription}
          onRuntime={setRuntime}
          onTimeout={setTimeoutSecs}
          onTagsInput={setTagsInput}
          onTargetAgent={setTargetAgentId}
          onSubmit={handleSave}
          onTestRun={handleTestRun}
        />

        {/* Code editor */}
        <div className="lg:col-span-2 rounded-lg border border-slate-800 bg-slate-900 overflow-hidden flex flex-col">
          <div className="px-5 py-3 border-b border-slate-800 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileCode2 className="h-4 w-4 text-gray-300" />
              <h2 className="text-sm font-semibold text-white">Code</h2>
              <span className="text-xs text-gray-400">
                · {RUNTIME_OPTIONS.find((o) => o.value === runtime)?.label}
              </span>
            </div>
            <span className="text-xs text-gray-400 font-mono">
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
