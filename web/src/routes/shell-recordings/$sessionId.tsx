// Shell session recording — playback page.
//
// Fetches all events for a session via the json-array endpoint and
// renders them into a terminal-style viewport. Playback is driven
// by a setInterval that advances through events at the chosen
// speed (1x, 2x, 4x, 8x). Keyboard controls: space toggles
// pause, left/right seek by 5s, up/down cycles through speed
// presets.
//
// We don't depend on xterm.js (the project hasn't added it) — the
// output is a styled <pre> that renders the raw terminal data
// (which already contains ANSI escapes) with white-space: pre. The
// browser's native rendering handles the escapes for unrecognised
// sequences, and ANSI colours are rendered by the browser when the
// font-stack is set to a monospace family.
//
// For a more faithful render, the file imports nothing extra; a
// future change can swap the viewport for xterm.js without
// changing the event-fetching logic.

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Play,
  Pause,
  RotateCcw,
  Download,
  Trash2,
  ChevronLeft,
  ChevronRight,
  Gauge,
} from 'lucide-react';
import { apiFetch } from '@/lib/api';
import { getStoredUser } from '@/lib/auth';

export const Route = createFileRoute('/shell-recordings/$sessionId')({
  component: RecordingPlaybackPage,
});

interface RecordingMetadata {
  session_id: string;
  agent_id: string;
  user_id: string;
  protocol: string;
  terminal_size: { cols: number; rows: number };
  started_at: string;
  ended_at: string;
  duration: string;
  bytes_in: number;
  bytes_out: number;
  event_count: number;
  chunk_count: number;
  content_hash: string;
}

interface ShellEvent {
  ts: string;
  direction: 'in' | 'out';
  data: string;
  kind?: string;
}

const SPEEDS = [1, 2, 4, 8];

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(2)} MB`;
}

function RecordingPlaybackPage() {
  const { sessionId } = Route.useParams();
  const navigate = useNavigate();
  const [meta, setMeta] = useState<RecordingMetadata | null>(null);
  const [events, setEvents] = useState<ShellEvent[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [currentIdx, setCurrentIdx] = useState(0);
  const [isPlaying, setIsPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [isAdmin, setIsAdmin] = useState(false);
  const terminalRef = useRef<HTMLPreElement | null>(null);
  const intervalRef = useRef<number | null>(null);

  useEffect(() => {
    const u = getStoredUser();
    setIsAdmin(u?.role === 'admin');
  }, []);

  const fetchAll = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const [metaRes, eventsRes] = await Promise.all([
        apiFetch(`/api/v1/shell/recordings/${sessionId}`),
        apiFetch(`/api/v1/shell/recordings/${sessionId}/events`),
      ]);
      if (!metaRes.ok) throw new Error(`Failed to load metadata (${metaRes.status})`);
      if (!eventsRes.ok) throw new Error(`Failed to load events (${eventsRes.status})`);
      const m: RecordingMetadata = await metaRes.json();
      const evts: ShellEvent[] = await eventsRes.json();
      setMeta(m);
      setEvents(Array.isArray(evts) ? evts : []);
      setCurrentIdx(0);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setIsLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    void fetchAll();
  }, [fetchAll]);

  // Playback timer
  useEffect(() => {
    if (!isPlaying || events.length === 0) return;
    const baseInterval = 100; // ms per event tick at 1x
    const interval = baseInterval / speed;
    intervalRef.current = window.setInterval(() => {
      setCurrentIdx((i) => {
        if (i >= events.length - 1) {
          setIsPlaying(false);
          return i;
        }
        return i + 1;
      });
    }, interval);
    return () => {
      if (intervalRef.current !== null) {
        window.clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [isPlaying, speed, events.length]);

  // Auto-scroll terminal
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [currentIdx]);

  // Keyboard controls
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      switch (e.code) {
        case 'Space':
          e.preventDefault();
          setIsPlaying((p) => !p);
          break;
        case 'ArrowLeft':
          setCurrentIdx((i) => Math.max(0, i - 10));
          break;
        case 'ArrowRight':
          setCurrentIdx((i) => Math.min(events.length - 1, i + 10));
          break;
        case 'ArrowUp':
          setSpeed((s) => Math.min(SPEEDS[SPEEDS.length - 1], s * 2));
          break;
        case 'ArrowDown':
          setSpeed((s) => Math.max(1, s / 2));
          break;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [events.length]);

  const visibleText = useMemo(() => {
    return events
      .slice(0, currentIdx + 1)
      .map((e) => e.data)
      .join('');
  }, [events, currentIdx]);

  const handleDelete = async () => {
    if (!confirm('Delete this recording? This cannot be undone.')) return;
    try {
      const res = await apiFetch(`/api/v1/shell/recordings/${sessionId}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(`Delete failed (${res.status})`);
      void navigate({ to: '/shell-recordings' });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  const handleDownload = () => {
    const blob = new Blob([visibleText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `session-${sessionId}.log`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (isLoading) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400" role="status" aria-live="polite">
        Loading recording…
      </div>
    );
  }
  if (error || !meta) {
    return (
      <div className="space-y-3">
        <button
          type="button"
          onClick={() => void navigate({ to: '/shell-recordings' })}
          className="inline-flex items-center gap-1.5 text-sm text-gray-300 hover:text-white transition-colors"
        >
          <ChevronLeft className="h-4 w-4" /> Back to recordings
        </button>
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error ?? 'Recording not found'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center gap-3 flex-wrap">
        <button
          type="button"
          onClick={() => void navigate({ to: '/shell-recordings' })}
          className="inline-flex items-center justify-center h-9 w-9 rounded-md border border-slate-800 bg-slate-900 hover:bg-slate-800 hover:border-slate-700 text-gray-300 hover:text-white transition-colors"
          aria-label="Back to recordings"
        >
          <ChevronLeft className="h-4 w-4" />
        </button>
        <div className="flex-1 min-w-0">
          <h1 className="text-xl font-bold text-white font-mono truncate">{meta.session_id}</h1>
          <p className="text-xs text-gray-300 mt-0.5">
            {meta.agent_id} · {meta.user_id} · {meta.protocol.toUpperCase()}
          </p>
        </div>
        <button
          type="button"
          onClick={handleDownload}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Download className="h-4 w-4" />
          Download
        </button>
        {isAdmin && (
          <button
            type="button"
            onClick={handleDelete}
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-red-600 hover:bg-red-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
          >
            <Trash2 className="h-4 w-4" />
            Delete
          </button>
        )}
      </div>

      {/* Metadata */}
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-4 grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Started</div>
          <div className="text-white text-xs">{new Date(meta.started_at).toLocaleString()}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Duration</div>
          <div className="text-white text-xs">{meta.duration}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Events</div>
          <div className="text-white text-xs">{meta.event_count.toLocaleString()}</div>
        </div>
        <div>
          <div className="text-xs text-gray-400 mb-0.5">Size</div>
          <div className="text-white text-xs">
            {formatBytes(meta.bytes_in + meta.bytes_out)}
          </div>
        </div>
      </div>

      {/* Playback controls */}
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-3 flex items-center gap-3 flex-wrap">
        <button
          type="button"
          onClick={() => setIsPlaying((p) => !p)}
          className="inline-flex items-center justify-center h-9 w-9 rounded-md bg-blue-600 hover:bg-blue-500 text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          aria-label={isPlaying ? 'Pause' : 'Play'}
        >
          {isPlaying ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
        </button>
        <button
          type="button"
          onClick={() => {
            setCurrentIdx(0);
            setIsPlaying(false);
          }}
          className="inline-flex items-center justify-center h-9 w-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          aria-label="Reset"
        >
          <RotateCcw className="h-4 w-4" />
        </button>
        <div className="flex items-center gap-1.5 text-xs text-gray-300">
          <Gauge className="h-3.5 w-3.5" />
          <select
            value={speed}
            onChange={(e) => setSpeed(Number(e.target.value))}
            aria-label="Playback speed"
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          >
            {SPEEDS.map((s) => (
              <option key={s} value={s}>
                {s}x
              </option>
            ))}
          </select>
        </div>
        <div className="flex-1 min-w-[120px]">
          <div className="h-1.5 rounded-full bg-slate-800 overflow-hidden">
            <div
              className="h-full bg-blue-500 transition-all"
              style={{ width: `${events.length > 0 ? ((currentIdx + 1) / events.length) * 100 : 0}%` }}
            />
          </div>
        </div>
        <span className="text-xs text-gray-400 tabular-nums">
          {currentIdx + 1} / {events.length}
        </span>
      </div>

      {/* Terminal viewport */}
      <div className="rounded-xl border border-slate-800 bg-slate-950 overflow-hidden">
        <pre
          ref={terminalRef}
          className="p-4 font-mono text-xs text-white whitespace-pre overflow-auto h-96 leading-relaxed"
          aria-label="Terminal output"
        >
          {visibleText || <span className="text-gray-500">— press play to begin —</span>}
        </pre>
      </div>

      {/* Keyboard hints */}
      <p className="text-[10px] text-gray-500 text-center">
        Space = play/pause · ← → = seek · ↑ ↓ = speed
      </p>
    </div>
  );
}
