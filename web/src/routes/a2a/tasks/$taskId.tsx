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
      return { classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20', icon: <CircleDot className="h-3 w-3" />, label: 'Pending' };
    case 'working':
      return { classes: 'bg-info/10 text-info border-info/20', icon: <Loader2 className="h-3 w-3 animate-spin" />, label: 'Working' };
    case 'input_required':
      return { classes: 'bg-warning/10 text-warning border-warning/20', icon: <AlertCircle className="h-3 w-3" />, label: 'Input Required' };
    case 'completed':
      return { classes: 'bg-success/10 text-success border-success/20', icon: <CheckCircle2 className="h-3 w-3" />, label: 'Completed' };
    case 'failed':
      return { classes: 'bg-danger/10 text-danger border-danger/20', icon: <XCircle className="h-3 w-3" />, label: 'Failed' };
    case 'cancelled':
      return { classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20', icon: <StopCircle className="h-3 w-3" />, label: 'Cancelled' };
  }
}

function fmt(iso?: string): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString();
}

function fmtDuration(ms?: number): string {
  if (ms === undefined) return '—';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function fmtCost(c?: number): string {
  if (c === undefined) return '—';
  return `$${c.toFixed(4)}`;
}

function fmtTokens(n?: number): string {
  if (n === undefined) return '—';
  return n.toLocaleString();
}

function downloadArtifact(a: A2AArtifact) {
  // Build a combined text + data representation. File parts are kept as-is
  // for the browser to handle; text and data parts are bundled into a
  // single text blob for download.
  const lines: string[] = [];
  lines.push(`# ${a.name}`);
  if (a.description) lines.push(`\n${a.description}\n`);
  for (const p of a.parts) {
    if (p.type === 'text') lines.push(p.text ?? '');
    else if (p.type === 'data') lines.push('```json\n' + JSON.stringify(p.data, null, 2) + '\n```');
    else if (p.type === 'file') lines.push(`[file: ${p.filename ?? p.url}]`);
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `${a.name || 'artifact'}.txt`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function PartRenderer({ part }: { part: A2APart }) {
  if (part.type === 'text') {
    return (
      <div className="text-sm text-text-primary whitespace-pre-wrap font-sans leading-relaxed">
        {part.text}
      </div>
    );
  }
  if (part.type === 'file') {
    return (
      <div className="flex items-center gap-2 text-sm text-text-secondary">
        <FileText className="h-4 w-4 text-info" />
        <span className="font-mono">{part.filename ?? part.url ?? 'file'}</span>
        {part.mime_type && <span className="text-xs text-text-muted">({part.mime_type})</span>}
      </div>
    );
  }
  if (part.type === 'data') {
    return (
      <pre className="text-xs text-text-primary font-mono bg-surface-primary p-2 rounded overflow-x-auto">
        {JSON.stringify(part.data, null, 2)}
      </pre>
    );
  }
  return null;
}

function MessageBubble({ msg }: { msg: A2AMessage }) {
  const isUser = msg.role === 'user';
  const isAgent = msg.role === 'agent';
  return (
    <div className={`flex gap-3 ${isUser ? 'flex-row-reverse' : ''}`}>
      <div
        className={`flex-shrink-0 h-7 w-7 rounded-full flex items-center justify-center ${
          isUser ? 'bg-accent' : isAgent ? 'bg-success' : 'bg-border-strong'
        }`}
      >
        {isUser ? <User className="h-3.5 w-3.5 text-white" /> : <Bot className="h-3.5 w-3.5 text-white" />}
      </div>
      <div
        className={`flex-1 rounded-lg p-3 ${
          isUser ? 'bg-accent/10 border border-accent/20' : 'bg-surface-tertiary border border-border-strong'
        }`}
      >
        <div className="text-[10px] uppercase tracking-wider text-text-muted mb-1.5">
          {msg.role}
          {msg.timestamp && (
            <span className="ml-2 normal-case font-normal">{fmt(msg.timestamp)}</span>
          )}
        </div>
        <div className="space-y-2">
          {msg.parts.map((p, i) => (
            <PartRenderer key={i} part={p} />
          ))}
        </div>
      </div>
    </div>
  );
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
      // Refetch to get the updated status.
      const t = await fetchTask(taskId);
      setTask(t);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cancel failed');
    } finally {
      setIsCancelling(false);
    }
  };

  if (isLoading) {
    return <div className="text-center py-12 text-text-secondary text-sm">Loading task...</div>;
  }
  if (error || !task) {
    return (
      <div className="space-y-3">
        <Link
          to="/a2a/tasks"
          className="inline-flex items-center gap-1 text-sm text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" /> Back to tasks
        </Link>
        <div className="p-3 rounded-md border border-danger/30 bg-danger/10 text-danger text-sm">
          {error ?? 'Task not found'}
        </div>
      </div>
    );
  }

  const badge = statusBadge(task.status);
  const canCancel = task.status === 'pending' || task.status === 'working' || task.status === 'input_required';

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Link
          to="/a2a/tasks"
          className="p-1.5 rounded-md hover:bg-surface-tertiary text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <div className="flex-1 min-w-0">
          <h1 className="text-xl font-semibold text-text-primary flex items-center gap-2 truncate">
            <Hash className="h-4 w-4 text-text-muted flex-shrink-0" />
            <span className="font-mono truncate">{task.id}</span>
          </h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Adapter: <span className="text-accent">{task.adapter}</span>
            {task.model && (
              <span>
                {' '}
                · Model: <span className="text-text-secondary">{task.model}</span>
              </span>
            )}
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
            className="px-3 py-1.5 text-xs rounded border border-danger/30 text-danger bg-danger/10 hover:bg-danger/20 disabled:opacity-50"
          >
            {isCancelling ? 'Cancelling...' : 'Cancel'}
          </button>
        )}
      </div>

      {/* Timestamps */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-4 grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <Field label="Created" value={fmt(task.created_at)} />
        <Field label="Updated" value={fmt(task.updated_at)} />
        <Field label="Completed" value={fmt(task.completed_at)} />
        <Field label="Duration" value={fmtDuration(task.duration_ms)} />
      </div>

      {/* Messages */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-4">Messages</h2>
        {task.messages && task.messages.length > 0 ? (
          <div className="space-y-3">
            {task.messages.map((m, i) => (
              <MessageBubble key={i} msg={m} />
            ))}
          </div>
        ) : (
          <p className="text-sm text-text-muted">No messages.</p>
        )}
      </div>

      {/* Artifacts */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <FileCode2 className="h-4 w-4" /> Artifacts
        </h2>
        {task.artifacts && task.artifacts.length > 0 ? (
          <div className="space-y-3">
            {task.artifacts.map((a) => (
              <div key={a.id} className="rounded border border-border-strong bg-surface-primary p-3">
                <div className="flex items-center justify-between mb-2">
                  <div>
                    <div className="text-sm font-medium text-text-primary">{a.name}</div>
                    {a.description && (
                      <div className="text-xs text-text-muted">{a.description}</div>
                    )}
                  </div>
                  <button
                    type="button"
                    onClick={() => downloadArtifact(a)}
                    className="flex items-center gap-1 text-xs px-2 py-1 rounded bg-surface-tertiary hover:bg-border-strong text-text-primary"
                  >
                    <Download className="h-3 w-3" /> Download
                  </button>
                </div>
                <div className="space-y-2">
                  {a.parts.map((p, i) => (
                    <PartRenderer key={i} part={p} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-text-muted">No artifacts generated.</p>
        )}
      </div>

      {/* Cost breakdown */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <Coins className="h-4 w-4" /> Cost Breakdown
        </h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
          <Field label="Prompt tokens" value={fmtTokens(task.prompt_tokens)} />
          <Field label="Completion tokens" value={fmtTokens(task.completion_tokens)} />
          <Field label="Total tokens" value={fmtTokens(task.total_tokens)} />
          <Field label="Total cost" value={fmtCost(task.cost)} highlight />
        </div>
      </div>

      {/* Metadata */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary p-5">
        <h2 className="text-sm font-semibold text-text-primary uppercase tracking-wider mb-3 flex items-center gap-2">
          <Database className="h-4 w-4" /> Metadata
        </h2>
        <pre className="text-xs text-text-secondary font-mono bg-surface-primary p-3 rounded overflow-x-auto max-h-64 overflow-y-auto">
          {JSON.stringify(task.metadata ?? {}, null, 2)}
        </pre>
      </div>
    </div>
  );
}

function Field({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div>
      <div className="text-xs text-text-muted mb-0.5">{label}</div>
      <div className={highlight ? 'text-base font-semibold text-accent' : 'text-text-primary'}>
        {value}
      </div>
    </div>
  );
}
