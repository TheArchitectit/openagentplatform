// A2A Task Detail — conversation thread, generated artifacts, cost
// breakdown, and raw metadata for a single A2A task.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import {
  ArrowLeft,
  CircleDot,
  CheckCircle2,
  XCircle,
  AlertCircle,
  StopCircle,
  Loader2,
  Download,
  FileText,
  FileCode2,
  Database,
  Coins,
  Hash,
  User,
  Bot,
} from 'lucide-react';
import { fetchTask, cancelTask, type A2ATask, type A2AMessage, type A2APart, type A2AArtifact } from '@/lib/useA2A';

export const Route = createFileRoute('/a2a/tasks/$taskId')({
  component: TaskDetailPage,
});

function statusBadge(status: A2ATask['status']): { classes: string; icon: React.ReactNode; label: string } {
  switch (status) {
    case 'pending':
      return { classes: 'bg-slate-500/10 text-gray-300 border-slate-500/20', icon: <CircleDot className="h-3 w-3" />, label: 'Pending' };
    case 'working':
      return { classes: 'bg-blue-500/10 text-blue-400 border-blue-500/20', icon: <Loader2 className="h-3 w-3 animate-spin" />, label: 'Working' };
    case 'completed':
      return { classes: 'bg-green-500/10 text-green-400 border-green-500/20', icon: <CheckCircle2 className="h-3 w-3" />, label: 'Completed' };
    case 'failed':
      return { classes: 'bg-red-500/10 text-red-400 border-red-500/20', icon: <XCircle className="h-3 w-3" />, label: 'Failed' };
    case 'cancelled':
      return { classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20', icon: <StopCircle className="h-3 w-3" />, label: 'Cancelled' };
    default:
      return { classes: 'bg-slate-500/10 text-gray-300 border-slate-500/20', icon: <AlertCircle className="h-3 w-3" />, label: status };
  }
}

function partPreview(part: A2APart): string {
  switch (part.kind) {
    case 'text':
      return part.text ?? '';
    case 'file':
      return `[File: ${part.file?.name ?? 'unnamed'}]`;
    case 'data':
      return `[Data: ${JSON.stringify(part.data ?? {}).slice(0, 80)}]`;
    default:
      return '';
  }
}

function artifactIcon(kind: string) {
  if (kind.includes('code') || kind.includes('script')) return <FileCode2 className="h-3.5 w-3.5" />;
  if (kind.includes('data') || kind.includes('json')) return <Database className="h-3.5 w-3.5" />;
  return <FileText className="h-3.5 w-3.5" />;
}

function TaskDetailPage() {
  const { taskId } = Route.useParams();
  const [task, setTask] = useState<A2ATask | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isCancelling, setIsCancelling] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);
    void (async () => {
      try {
        const t = await fetchTask(taskId);
        if (cancelled) return;
        setTask(t);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Failed to load task');
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [taskId]);

  const handleCancel = async () => {
    if (isCancelling || !task) return;
    setIsCancelling(true);
    try {
      await cancelTask(taskId);
      const updated = await fetchTask(taskId);
      setTask(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cancel failed');
    } finally {
      setIsCancelling(false);
    }
  };

  const handleDownloadArtifact = (artifact: A2AArtifact) => {
    const blob = new Blob([JSON.stringify(artifact, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `artifact-${artifact.name ?? taskId}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (isLoading) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status" aria-live="polite">
        Loading task…
      </div>
    );
  }
  if (error || !task) {
    return (
      <div className="space-y-3">
        <Link
          to="/a2a/tasks"
          className="inline-flex items-center gap-1.5 text-sm text-gray-300 hover:text-white transition-colors"
        >
          <ArrowLeft className="h-4 w-4" /> Back to task list
        </Link>
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error ?? 'Task not found'}
        </div>
      </div>
    );
  }

  const badge = statusBadge(task.status);
  const canCancel = task.status === 'pending' || task.status === 'working';

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center gap-3 flex-wrap">
        <Link
          to="/a2a/tasks"
          className="inline-flex items-center justify-center h-9 w-9 rounded-md border border-slate-800 bg-slate-900 hover:bg-slate-800 hover:border-slate-700 text-gray-300 hover:text-white transition-colors"
          aria-label="Back to task list"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <div className="flex-1 min-w-0">
          <h1 className="text-2xl font-bold text-white truncate font-mono">{task.id}</h1>
          <p className="text-sm text-gray-300 mt-0.5">
            {task.adapter ?? '—'} · {new Date(task.created_at).toLocaleString()}
          </p>
        </div>
        <span className={`inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded border ${badge.classes}`}>
          {badge.icon}
          {badge.label}
        </span>
        {canCancel && (
          <button
            type="button"
            onClick={handleCancel}
            disabled={isCancelling}
            className="inline-flex items-center gap-1.5 px-3 h-9 text-sm rounded-md bg-red-600 hover:bg-red-500 text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
          >
            {isCancelling ? <Loader2 className="h-4 w-4 animate-spin" /> : <StopCircle className="h-4 w-4" />}
            Cancel
          </button>
        )}
      </div>

      {/* Metadata grid */}
      <section className="rounded-lg border border-slate-800 bg-slate-900 p-4 grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm" aria-label="Task metadata">
        <div>
          <div className="text-xs text-gray-400 mb-0.5 flex items-center gap-1">
            <Hash className="h-3 w-3" /> ID
          </div>
          <div className="text-white font-mono text-xs break-all">{task.id}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5 flex items-center gap-1">
            <Coins className="h-3 w-3" /> Cost
          </div>
          <div className="text-white">{task.cost !== undefined ? `$${task.cost.toFixed(4)}` : '—'}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Model</div>
          <div className="text-white">{task.model ?? '—'}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Tokens</div>
          <div className="text-white">
            {task.input_tokens !== undefined
              ? `${task.input_tokens.toLocaleString()} in / ${task.output_tokens?.toLocaleString() ?? '—'} out`
              : '—'}
          </div>
        </div>
      </section>

      {/* Conversation thread */}
      <section className="rounded-lg border border-slate-800 bg-slate-900 p-4" aria-label="Conversation">
        <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Conversation</h2>
        {task.messages && task.messages.length > 0 ? (
          <div className="space-y-3">
            {task.messages.map((msg: A2AMessage, i: number) => (
              <MessageBubble key={i} message={msg} />
            ))}
          </div>
        ) : (
          <p className="text-sm text-gray-400">No messages in this task.</p>
        )}
      </section>

      {/* Artifacts */}
      {task.artifacts && task.artifacts.length > 0 && (
        <section className="rounded-lg border border-slate-800 bg-slate-900 p-4" aria-label="Artifacts">
          <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Artifacts</h2>
          <div className="space-y-2">
            {task.artifacts.map((artifact: A2AArtifact, i: number) => (
              <div
                key={i}
                className="flex items-center justify-between gap-3 rounded-md border border-slate-800 bg-slate-800/40 p-3"
              >
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-blue-400 flex-shrink-0">{artifactIcon(artifact.kind ?? '')}</span>
                  <div className="min-w-0">
                    <div className="text-sm text-white truncate">{artifact.name ?? `artifact-${i}`}</div>
                    <div className="text-xs text-gray-400">
                      {artifact.kind ?? 'file'} · {artifact.parts?.length ?? 0} part(s)
                    </div>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => handleDownloadArtifact(artifact)}
                  className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors"
                  aria-label="Download artifact"
                >
                  <Download className="h-3 w-3" />
                  Download
                </button>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Raw JSON */}
      <section className="rounded-lg border border-slate-800 bg-slate-900 p-4" aria-label="Raw task data">
        <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Raw Task Data</h2>
        <pre className="text-xs text-white font-mono whitespace-pre-wrap overflow-x-auto max-h-96 overflow-y-auto rounded-md border border-slate-800 bg-slate-800/40 p-3">
          {JSON.stringify(task, null, 2)}
        </pre>
      </section>
    </div>
  );
}

function MessageBubble({ message }: { message: A2AMessage }) {
  const isUser = message.role === 'user';
  return (
    <div className={`flex gap-2.5 ${isUser ? 'flex-row-reverse' : ''}`}>
      <div
        className={`h-7 w-7 rounded-full flex items-center justify-center flex-shrink-0 ${
          isUser ? 'bg-blue-600/20 text-blue-400' : 'bg-slate-800 text-gray-300'
        }`}
        aria-hidden="true"
      >
        {isUser ? <User className="h-3.5 w-3.5" /> : <Bot className="h-3.5 w-3.5" />}
      </div>
      <div
        className={`rounded-lg border p-3 max-w-[80%] ${
          isUser
            ? 'bg-blue-600/10 border-blue-500/20 text-white'
            : 'bg-slate-800/40 border-slate-800 text-gray-300'
        }`}
      >
        <div className="text-[10px] uppercase tracking-wider text-gray-400 mb-1">
          {message.role}
        </div>
        {message.parts.map((p: A2APart, i: number) => (
          <div key={i} className="text-sm whitespace-pre-wrap">
            {partPreview(p)}
          </div>
        ))}
      </div>
    </div>
  );
}
