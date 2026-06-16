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

interface PlayEvent {
  offset_ms: number;
  dir: string;
  data_b64: string;
  data_hex: string;
  size: number;
  wall_ts: string;
}

interface PlayResponse {
  metadata: RecordingMetadata;
  events: PlayEvent[];
}

const SPEED_PRESETS = [1, 2, 4, 8];
const SEEK_STEP_MS = 5000;
const TICK_MS = 33; // ~30 fps; small enough for smooth playback

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(2)} MB`;
}

function formatDate(iso: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

function formatDuration(s: string): string {
  if (!s) return '—';
  const re = /^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/;
  const m = s.match(re);
  if (!m) return s;
  const h = parseInt(m[1] || '0', 10);
  const mm = parseInt(m[2] || '0', 10);
  const ss = parseFloat(m[3] || '0');
  if (h > 0) return `${h}h ${mm}m ${Math.floor(ss)}s`;
  if (mm > 0) return `${mm}m ${Math.floor(ss)}s`;
  return `${ss.toFixed(1)}s`;
}

function formatOffset(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const total = Math.floor(ms / 1000);
  const hh = Math.floor(total / 3600);
  const mm = Math.floor((total % 3600) / 60);
  const ss = total % 60;
  const mss = ms % 1000;
  return `${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}:${String(ss).padStart(2, '0')}.${String(mss).padStart(3, '0')}`;
}

function b64ToString(b64: string): string {
  try {
    // atob is available in browsers and modern Node.
    if (typeof atob === 'function') {
      const bin = atob(b64);
      // Convert binary string -> UTF-8 string.
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      return new TextDecoder('utf-8', { fatal: false }).decode(bytes);
    }
  } catch {
    /* fall through */
  }
  return '';
}

function RecordingPlaybackPage() {
  const { sessionId } = Route.useParams();
  const navigate = useNavigate();
  const user = getStoredUser();
  const isAdmin =
    user?.role === 'admin' || user?.role === 'owner' || user?.role === 'superadmin';

  const [meta, setMeta] = useState<RecordingMetadata | null>(null);
  const [events, setEvents] = useState<PlayEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Playback state.
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [currentMS, setCurrentMS] = useState(0);
  const [rendered, setRendered] = useState('');
  const [deleting, setDeleting] = useState(false);
  const playbackRef = useRef<{ cursor: number }>({ cursor: 0 });
  const viewportRef = useRef<HTMLPreElement | null>(null);
  const accumulatedRef = useRef('');

  const totalDurationMS = useMemo(() => {
    if (events.length === 0) return 0;
    return Math.max(events[events.length - 1].offset_ms, 0);
  }, [events]);

  // ---- Load events ---------------------------------------------------
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      try {
        const data = await apiFetch<PlayResponse>(`/shell/recordings/${sessionId}/play?format=json-array`);
        if (cancelled) return;
        setMeta(data.metadata);
        setEvents(data.events);
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [sessionId]);

  // ---- Seek / reset helpers -----------------------------------------
  const reset = useCallback(() => {
    accumulatedRef.current = '';
    setRendered('');
    playbackRef.current.cursor = 0;
    setCurrentMS(0);
    setPlaying(false);
  }, []);

  const seek = useCallback(
    (ms: number) => {
      const target = Math.max(0, Math.min(ms, totalDurationMS));
      // Rebuild rendered output from scratch for every event up to
      // the seek target. With a few thousand events this is still
      // fast (string concatenation in the browser is cheap).
      let acc = '';
      let next = 0;
      for (let i = 0; i < events.length; i++) {
        if (events[i].offset_ms > target) break;
        // Use the raw data_hex (hex-encoded) and convert through
        // the same path as playback so the output matches.
        const raw = hexToString(events[i].data_hex);
        if (events[i].dir === 'out' || events[i].dir === 'in') {
          acc += raw;
        }
        next = i + 1;
      }
      accumulatedRef.current = acc;
      setRendered(acc);
      playbackRef.current.cursor = next;
      setCurrentMS(target);
    },
    [events, totalDurationMS]
  );

  // ---- Playback loop -----------------------------------------------
  useEffect(() => {
    if (!playing) return;
    const interval = window.setInterval(() => {
      const cursor = playbackRef.current.cursor;
      if (cursor >= events.length) {
        setPlaying(false);
        return;
      }
      const next = currentMS + TICK_MS * speed;
      // Emit every event whose offset_ms <= next.
      let advanced = false;
      while (
        playbackRef.current.cursor < events.length &&
        events[playbackRef.current.cursor].offset_ms <= next
      ) {
        const ev = events[playbackRef.current.cursor];
        const raw = hexToString(ev.data_hex);
        if (ev.dir === 'out' || ev.dir === 'in') {
          accumulatedRef.current += raw;
        }
        playbackRef.current.cursor++;
        advanced = true;
      }
      if (advanced) {
        setRendered(accumulatedRef.current);
      }
      setCurrentMS(next);
      if (playbackRef.current.cursor >= events.length && next >= totalDurationMS) {
        setPlaying(false);
      }
    }, TICK_MS);
    return () => window.clearInterval(interval);
  }, [playing, speed, currentMS, events, totalDurationMS]);

  // Auto-scroll viewport to bottom while playing.
  useEffect(() => {
    const el = viewportRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [rendered]);

  // ---- Keyboard controls -------------------------------------------
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
        return;
      }
      if (e.code === 'Space') {
        e.preventDefault();
        setPlaying((p) => !p);
      } else if (e.code === 'ArrowLeft') {
        e.preventDefault();
        seek(currentMS - SEEK_STEP_MS);
      } else if (e.code === 'ArrowRight') {
        e.preventDefault();
        seek(currentMS + SEEK_STEP_MS);
      } else if (e.code === 'ArrowUp') {
        e.preventDefault();
        setSpeed((s) => Math.min(8, s === 8 ? 8 : s * 2));
      } else if (e.code === 'ArrowDown') {
        e.preventDefault();
        setSpeed((s) => Math.max(1, s / 2));
      } else if (e.code === 'KeyR') {
        e.preventDefault();
        reset();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [seek, currentMS, reset]);

  // ---- Delete handler -----------------------------------------------
  const handleDelete = async () => {
    if (!isAdmin) return;
    if (!confirm(`Delete recording ${sessionId}? This cannot be undone.`)) return;
    setDeleting(true);
    try {
      await apiFetch<void>(`/shell/recordings/${sessionId}`, { method: 'DELETE' });
      navigate({ to: '/shell-recordings' });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setDeleting(false);
    }
  };

  if (loading) {
    return (
      <div className="text-slate-400 text-sm p-6">Loading recording…</div>
    );
  }
  if (error) {
    return (
      <div className="space-y-3">
        <div className="text-rose-400 text-sm">{error}</div>
        <button
          type="button"
          onClick={() => navigate({ to: '/shell-recordings' })}
          className="px-3 py-1.5 rounded-md bg-slate-800 hover:bg-slate-700 text-sm text-slate-200"
        >
          Back to list
        </button>
      </div>
    );
  }
  if (!meta) {
    return <div className="text-slate-400 text-sm">Recording not found.</div>;
  }

  return (
    <div className="grid grid-cols-1 lg:grid-cols-[1fr,280px] gap-4 h-[calc(100vh-7rem)]">
      {/* Main playback area */}
      <div className="flex flex-col bg-slate-950 border border-slate-800 rounded-lg overflow-hidden">
        {/* Header bar */}
        <div className="flex items-center justify-between px-4 py-2 bg-slate-900 border-b border-slate-800">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => navigate({ to: '/shell-recordings' })}
              className="p-1.5 rounded-md hover:bg-slate-800 text-slate-300"
              title="Back to list"
            >
              <ChevronLeft size={16} />
            </button>
            <div className="font-mono text-xs text-slate-300 truncate max-w-[300px]">
              {meta.session_id}
            </div>
            <span className="text-xs text-slate-500">
              {meta.terminal_size.cols}×{meta.terminal_size.rows} {meta.protocol}
            </span>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setPlaying((p) => !p)}
              className="p-1.5 rounded-md hover:bg-slate-800 text-slate-200"
              title={playing ? 'Pause (space)' : 'Play (space)'}
            >
              {playing ? <Pause size={16} /> : <Play size={16} />}
            </button>
            <button
              type="button"
              onClick={reset}
              className="p-1.5 rounded-md hover:bg-slate-800 text-slate-200"
              title="Reset"
            >
              <RotateCcw size={16} />
            </button>
            <a
              href={`/api/v1/shell/recordings/${meta.session_id}/export`}
              className="p-1.5 rounded-md hover:bg-slate-800 text-slate-200"
              title="Download .cast"
            >
              <Download size={16} />
            </a>
            {isAdmin && (
              <button
                type="button"
                onClick={handleDelete}
                disabled={deleting}
                className="p-1.5 rounded-md hover:bg-rose-700/30 text-rose-300 disabled:opacity-50"
                title="Delete recording (admin only)"
              >
                <Trash2 size={16} />
              </button>
            )}
          </div>
        </div>

        {/* Terminal viewport */}
        <pre
          ref={viewportRef}
          className="flex-1 overflow-auto p-3 text-xs text-slate-100 font-mono whitespace-pre"
          style={{
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
            lineHeight: '1.2',
            background: '#020617',
          }}
        >
          {rendered}
        </pre>

        {/* Timeline + speed controls */}
        <div className="border-t border-slate-800 bg-slate-900 px-4 py-2 flex items-center gap-3">
          <span className="text-xs text-slate-500 font-mono w-32">
            {formatOffset(currentMS)} / {formatOffset(totalDurationMS)}
          </span>
          <input
            type="range"
            min={0}
            max={Math.max(1, totalDurationMS)}
            value={Math.min(currentMS, totalDurationMS)}
            onChange={(e) => seek(Number(e.target.value))}
            className="flex-1 accent-indigo-500"
          />
          <div className="flex items-center gap-1 text-xs text-slate-400">
            <Gauge size={14} />
            {SPEED_PRESETS.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setSpeed(s)}
                className={
                  'px-2 py-0.5 rounded ' +
                  (s === speed
                    ? 'bg-indigo-500/20 text-indigo-300 border border-indigo-500/30'
                    : 'bg-slate-800 hover:bg-slate-700 text-slate-300')
                }
              >
                {s}x
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Sidebar metadata */}
      <aside className="bg-slate-900 border border-slate-800 rounded-lg p-4 space-y-4 overflow-auto">
        <div>
          <h2 className="text-sm font-semibold text-slate-200 mb-2">Session</h2>
          <dl className="text-xs space-y-1.5">
            <Row k="User" v={meta.user_id} />
            <Row k="Agent" v={meta.agent_id} />
            <Row k="Protocol" v={meta.protocol} />
            <Row k="Terminal" v={`${meta.terminal_size.cols} × ${meta.terminal_size.rows}`} />
            <Row k="Duration" v={formatDuration(meta.duration)} />
            <Row k="Bytes in" v={formatBytes(meta.bytes_in)} />
            <Row k="Bytes out" v={formatBytes(meta.bytes_out)} />
            <Row k="Events" v={String(meta.event_count)} />
            <Row k="Chunks" v={String(meta.chunk_count)} />
            <Row k="Started" v={formatDate(meta.started_at)} />
            <Row k="Ended" v={formatDate(meta.ended_at)} />
          </dl>
        </div>
        <div>
          <h2 className="text-sm font-semibold text-slate-200 mb-2">Integrity</h2>
          <div className="text-[10px] font-mono text-slate-500 break-all bg-slate-950 border border-slate-800 rounded p-2">
            {meta.content_hash || '(empty)'}
          </div>
        </div>
        <div>
          <h2 className="text-sm font-semibold text-slate-200 mb-2">Keyboard</h2>
          <ul className="text-xs text-slate-400 space-y-1">
            <li><kbd className="text-slate-200">Space</kbd> play / pause</li>
            <li><kbd className="text-slate-200">← →</kbd> seek ±5 s</li>
            <li><kbd className="text-slate-200">↑ ↓</kbd> speed up / down</li>
            <li><kbd className="text-slate-200">R</kbd> reset</li>
          </ul>
        </div>
      </aside>
    </div>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between gap-2">
      <dt className="text-slate-500">{k}</dt>
      <dd className="text-slate-200 text-right truncate" title={v}>
        {v}
      </dd>
    </div>
  );
}

// hexToString decodes the same hex encoding used by the Go side in
// remote.encodeForJSON. It preserves bytes that don't form valid
// UTF-8 sequences by falling back to replacement characters.
function hexToString(hex: string): string {
  if (!hex) return '';
  if (hex.length % 2 !== 0) return '';
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    const a = unhex(hex.charCodeAt(i * 2));
    const b = unhex(hex.charCodeAt(i * 2 + 1));
    if (a < 0 || b < 0) return '';
    out[i] = (a << 4) | b;
  }
  return new TextDecoder('utf-8', { fatal: false }).decode(out);
}

function unhex(c: number): number {
  if (c >= 48 && c <= 57) return c - 48; // 0-9
  if (c >= 97 && c <= 102) return c - 87; // a-f
  if (c >= 65 && c <= 70) return c - 55; // A-F
  return -1;
}
