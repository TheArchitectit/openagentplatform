// Run detail — view the output of a single script run.
//
// Shows:
//   • Run metadata: script name, agent, status, start/end, duration, exit code
//   • Output viewer: terminal-like (black bg, monospace), stdout (white) +
//     stderr (red)
//   • Live output: when the run is in_progress, subscribes to the "scripts"
//     WebSocket channel for streamed output
//   • Cancel button (visible when in_progress)

import { createFileRoute, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeft,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  CirclePlay,
  Square,
  Loader2,
  Terminal,
  Bot,
  Clock,
  Hash,
  TerminalSquare,
  User,
} from 'lucide-react';
import { toast } from 'sonner';
import {
  useScripts,
  type ScriptRun,
  type ScriptRunStatus,
} from '@/lib/useScripts';
import { getWsClient, type WsEnvelope } from '@/lib/websocket';

export const Route = createFileRoute('/scripts/$scriptId/runs/$runId')({
  component: RunDetailPage,
});

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

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

function formatDuration(ms?: number, startedAt?: string, finishedAt?: string): string {
  if (ms !== undefined && ms !== null) {
    if (ms < 1000) return `${ms}ms`;
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    const rs = s % 60;
    return `${m}m ${rs}s`;
  }
  if (startedAt) {
    const start = new Date(startedAt).getTime();
    const end = finishedAt ? new Date(finishedAt).getTime() : Date.now();
    if (!isNaN(start) && !isNaN(end)) {
      return formatDuration(Math.max(0, end - start));
    }
  }
  return '—';
}

function isLive(status: ScriptRunStatus): boolean {
  return status === 'in_progress' || status === 'pending';
}

function RunDetailPage() {
  const { scriptId, runId } = Route.useParams();
  const { fetchRun, cancelRun } = useScripts();

  const [run, setRun] = useState<ScriptRun | null>(null);
  const [scriptName, setScriptName] = useState<string>('');
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Output buffers, appendable by WS events.
  const [stdout, setStdout] = useState<string>('');
  const [stderr, setStderr] = useState<string>('');
  const [now, setNow] = useState(() => Date.now());
  const [cancelling, setCancelling] = useState(false);

  const outputRef = useRef<HTMLDivElement>(null);

  // Keep relative durations fresh.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const reload = useCallback(async () => {
    try {
      const r = await fetchRun(runId);
      setRun(r);
      // The server may include the script name; fall back to "Unknown script"
      if ((r as ScriptRun & { script_name?: string }).script_name) {
        setScriptName((r as ScriptRun & { script_name?: string }).script_name!);
      }
      // Initialize output buffers from the initial fetch.
      setStdout(r.stdout ?? '');
      setStderr(r.stderr ?? '');
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIsLoading(false);
    }
  }, [runId, fetchRun]);

  useEffect(() => {
    setIsLoading(true);
    void reload();
  }, [reload]);

  // -----------------------------------------------------------------------
  // Live WS subscription for in-progress runs
  // -----------------------------------------------------------------------

  useEffect(() => {
    if (!run) return;
    if (!isLive(run.status)) return;

    const ws = getWsClient();

    const handler = (env: WsEnvelope) => {
      if (env.type !== 'event' || env.channel !== 'scripts') return;

      if (env.event === 'script.run.output') {
        const d = env.data as { run_id?: string; stream?: 'stdout' | 'stderr'; data?: string };
        if (!d || d.run_id !== runId) return;
        const chunk = d.data ?? '';
        if (d.stream === 'stderr') {
          setStderr((prev) => prev + chunk);
        } else {
          setStdout((prev) => prev + chunk);
        }
        return;
      }

      if (
        env.event === 'script.run.update' ||
        env.event === 'script.run.started' ||
        env.event === 'script.run.completed'
      ) {
        const r = env.data as ScriptRun;
        if (!r || r.id !== runId) return;
        setRun((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            ...r,
            // Keep any streamed output we already accumulated.
            stdout: r.stdout ?? prev.stdout,
            stderr: r.stderr ?? prev.stderr,
          };
        });
      }
    };

    const unsub = ws.subscribe('scripts', handler);
    return () => {
      unsub();
    };
  }, [run, runId]);

  // Auto-scroll output to bottom on update.
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [stdout, stderr]);

  // -----------------------------------------------------------------------
  // Cancel
  // -----------------------------------------------------------------------

  const onCancel = async () => {
    if (!run) return;
    if (!confirm('Cancel this run? The agent will receive a stop signal.')) return;
    setCancelling(true);
    try {
      await cancelRun(run.id);
      toast.success('Run cancelled');
      setRun((prev) => (prev ? { ...prev, status: 'cancelled' } : prev));
    } catch (e) {
      toast.error(`Cancel failed: ${(e as Error).message}`);
    } finally {
      setCancelling(false);
    }
  };

  // -----------------------------------------------------------------------
  // Render
  // -----------------------------------------------------------------------

  if (isLoading && !run) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
        <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
        Loading run…
      </div>
    );
  }

  if (!run) {
    return (
      <div className="space-y-4">
        <Link
          to="/scripts/$scriptId"
          params={{ scriptId }}
          className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"
        >
          <ArrowLeft className="h-4 w-4" /> Back to script
        </Link>
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-12 text-center text-slate-500">
          Run not found.
        </div>
      </div>
    );
  }

  const meta = STATUS_META[run.status] ?? STATUS_META.pending;
  const live = isLive(run.status);
  const liveDuration = live ? formatDuration(undefined, run.started_at, undefined) : null;
  const finalDuration = formatDuration(run.duration_ms, run.started_at, run.finished_at);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/scripts/$scriptId"
            params={{ scriptId }}
            className="p-2 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <Terminal className="h-4 w-4 text-slate-300" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold text-slate-100">
                {scriptName || `Run ${run.id.slice(0, 8)}`}
              </h1>
              <span
                className={
                  'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-xs ' + meta.classes
                }
              >
                {live ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <meta.icon className="h-3 w-3" />
                )}
                {meta.label}
              </span>
              {live && (
                <span className="inline-flex items-center gap-1 text-xs text-indigo-300">
                  <span className="inline-flex h-1.5 w-1.5 rounded-full bg-indigo-400 animate-pulse" />
                  live
                </span>
              )}
            </div>
            <p className="text-slate-400 text-sm mt-0.5">
              <Bot className="inline h-3.5 w-3.5 mr-1 text-slate-500" />
              {run.hostname ?? run.agent_id}
              {live && liveDuration ? (
                <>
                  <span className="mx-2 text-slate-600">•</span>
                  <span className="text-indigo-300">elapsed {liveDuration}</span>
                </>
              ) : (
                <>
                  <span className="mx-2 text-slate-600">•</span>
                  {finalDuration}
                </>
              )}
            </p>
          </div>
        </div>

        {live && (
          <button
            type="button"
            onClick={() => void onCancel()}
            disabled={cancelling}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-rose-500/30 bg-rose-500/10 text-rose-300 hover:bg-rose-500/20 text-sm disabled:opacity-50 transition-colors"
          >
            {cancelling ? <Loader2 className="h-4 w-4 animate-spin" /> : <Square className="h-4 w-4" />}
            <span>{cancelling ? 'Cancelling…' : 'Cancel Run'}</span>
          </button>
        )}
      </div>

      {error && (
        <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-300">
          {error}
        </div>
      )}

      {/* Metadata */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Script</p>
            <p className="text-sm text-slate-100 mt-1">
              {scriptName || <span className="text-slate-500 italic">Unknown</span>}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Agent</p>
            <p className="text-sm text-slate-100 mt-1">
              <Link
                to="/agents/$agentId"
                params={{ agentId: run.agent_id }}
                className="hover:text-indigo-300"
              >
                {run.hostname ?? run.agent_id}
              </Link>
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Status</p>
            <p className={'text-sm mt-1 capitalize ' + (meta.classes.split(' ')[1] ?? 'text-slate-300')}>
              {meta.label}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Exit Code</p>
            <p className="text-sm mt-1">
              {run.exit_code !== undefined && run.exit_code !== null ? (
                <span
                  className={
                    'tabular-nums font-mono ' +
                    (run.exit_code === 0 ? 'text-emerald-400' : 'text-rose-400')
                  }
                >
                  {run.exit_code}
                </span>
              ) : (
                <span className="text-slate-500">—</span>
              )}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Started</p>
            <p className="text-sm text-slate-100 mt-1">{formatDateTime(run.started_at)}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Finished</p>
            <p className="text-sm text-slate-100 mt-1">
              {live ? (
                <span className="text-indigo-300 inline-flex items-center gap-1">
                  <Clock className="h-3.5 w-3.5" />
                  in progress…
                </span>
              ) : (
                formatDateTime(run.finished_at)
              )}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Duration</p>
            <p className="text-sm text-slate-100 mt-1 tabular-nums">
              {live ? liveDuration : finalDuration}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wider text-slate-500">Triggered by</p>
            <p className="text-sm text-slate-100 mt-1">
              {run.triggered_by ? (
                <span className="inline-flex items-center gap-1">
                  <User className="h-3.5 w-3.5 text-slate-500" />
                  {run.triggered_by}
                </span>
              ) : (
                <span className="text-slate-500">—</span>
              )}
            </p>
          </div>
          <div className="sm:col-span-2 lg:col-span-4">
            <p className="text-xs uppercase tracking-wider text-slate-500">Run ID</p>
            <p className="text-xs text-slate-400 mt-1 font-mono inline-flex items-center gap-1">
              <Hash className="h-3 w-3" />
              {run.id}
            </p>
          </div>
        </div>
      </div>

      {/* Output viewer */}
      <div className="rounded-lg border border-slate-800 bg-black overflow-hidden flex flex-col">
        <div className="px-4 py-2 border-b border-slate-800 bg-slate-900 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <TerminalSquare className="h-4 w-4 text-slate-400" />
            <h2 className="text-sm font-semibold text-slate-200">Output</h2>
            {live && (
              <span className="inline-flex items-center gap-1 text-xs text-indigo-300">
                <span className="inline-flex h-1.5 w-1.5 rounded-full bg-indigo-400 animate-pulse" />
                streaming
              </span>
            )}
          </div>
          <span className="text-xs text-slate-500 font-mono">
            {(stdout.length + stderr.length).toLocaleString()} bytes
          </span>
        </div>
        <div
          ref={outputRef}
          className="bg-black text-slate-100 font-mono text-xs p-4 overflow-auto max-h-[36rem] whitespace-pre-wrap break-words leading-5"
        >
          {stdout && (
            <span className="text-slate-100">{stdout}</span>
          )}
          {stderr && (
            <span className="text-rose-400">{stderr}</span>
          )}
          {!stdout && !stderr && (
            <span className="text-slate-600 italic">
              {live ? 'Waiting for output…' : 'No output captured.'}
            </span>
          )}
        </div>
      </div>

      {/* Footer link back */}
      <div className="flex items-center justify-end">
        <Link
          to="/scripts/$scriptId"
          params={{ scriptId }}
          className="text-sm text-slate-400 hover:text-slate-100 transition-colors"
        >
          ← Back to script
        </Link>
      </div>
    </div>
  );
}
