// Script detail — view, edit, and run a script.
//
// Sections:
//   • Script info card: name, description, runtime, timeout, tags, timestamps
//   • Monaco code editor — read-only viewer by default; toggle to edit mode
//     (loads from jsDelivr CDN at runtime, falls back to textarea if offline)
//   • Run history table
//   • "Run Now" action — select target agent(s) and execute
//
// Edit mode PATCHes the script on save.

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
import { MonacoEditor, type MonacoLanguage } from '@/components/monaco-editor';

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
    classes: 'bg-success/10 text-success border-success/20',
  },
  powershell: {
    label: 'PowerShell',
    icon: Terminal,
    classes: 'bg-info/10 text-info border-info/20',
  },
  python: {
    label: 'Python',
    icon: Code2,
    classes: 'bg-warning/10 text-warning border-warning/20',
  },
  node: {
    label: 'Node',
    icon: Braces,
    classes: 'bg-accent/10 text-accent border-accent/20',
  },
};

/** Map a script runtime to the Monaco language id we want for syntax highlighting. */
const RUNTIME_TO_MONACO: Record<ScriptRuntime, MonacoLanguage> = {
  bash: 'bash',
  powershell: 'powershell',
  python: 'python',
  node: 'javascript',
};

const STATUS_META: Record<
  ScriptRunStatus,
  { label: string; classes: string; icon: typeof CircleCheck }
> = {
  pending: {
    label: 'Pending',
    classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20',
    icon: CircleDashed,
  },
  in_progress: {
    label: 'Running',
    classes: 'bg-accent/10 text-accent border-accent/20',
    icon: CirclePlay,
  },
  completed: {
    label: 'Success',
    classes: 'bg-success/10 text-success border-success/20',
    icon: CircleCheck,
  },
  failed: {
    label: 'Failed',
    classes: 'bg-danger/10 text-danger border-danger/20',
    icon: CircleX,
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20',
    icon: CircleDashed,
  },
  timeout: {
    label: 'Timeout',
    classes: 'bg-warning/10 text-warning border-warning/20',
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
      <div className="w-full max-w-md rounded-lg border border-border-subtle bg-surface-secondary shadow-xl">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Run Now</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-4 space-y-3">
          <div className="relative">
            <Globe className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search agents…"
              className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>
          <ul className="max-h-80 overflow-y-auto divide-y divide-border-subtle rounded-md border border-border-subtle">
            {filtered.length === 0 ? (
              <li className="px-3 py-6 text-center text-text-muted text-sm">
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
                      (isSelected ? 'bg-accent/10' : 'hover:bg-surface-tertiary/40')
                    }
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        onChange={() => onToggle(a.id)}
                        className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40"
                      />
                      <div className="min-w-0">
                        <p className="text-sm text-text-primary truncate">{a.hostname || a.id}</p>
                        <p className="text-xs text-text-muted truncate">
                          {a.os} · {a.status}
                        </p>
                      </div>
                    </div>
                  </li>
                );
              })
            )}
          </ul>
          <p className="text-xs text-text-muted">
            {selected.size} agent{selected.size === 1 ? '' : 's'} selected
          </p>
        </div>
        <div className="px-5 py-3 border-t border-border-subtle flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 h-9 rounded-md border border-border-strong bg-surface-tertiary text-sm text-text-primary hover:bg-border-strong transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={running || selected.size === 0}
            onClick={onRun}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent text-sm text-white disabled:opacity-50 transition-colors"
          >
            {running ? <Loader2 className="h-4 w-4 animate-spin" /> : <CirclePlay className="h-4 w-4" />}
            <span>{running ? 'Starting…' : 'Run'}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
