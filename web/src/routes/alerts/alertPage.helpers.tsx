// Shared constants and display helpers for the alerts inbox page.
import { SeverityBadge } from '@/components/severity-badge';

export const STATE_BADGES: Record<string, { label: string; classes: string }> = {
  open: { label: "Open", classes: "bg-blue-100 text-blue-800 border-blue-200" },
  acknowledged: { label: "Acknowledged", classes: "bg-yellow-100 text-yellow-800 border-yellow-200" },
  snoozed: { label: "Snoozed", classes: "bg-purple-100 text-purple-800 border-purple-200" },
  resolved: { label: "Resolved", classes: "bg-green-100 text-green-800 border-green-200" },
};
export const PAGE_SIZE = 20;
export const TABS = [
  { id: "all", label: "All" },
  { id: "critical", label: "Critical" },
  { id: "warning", label: "Warning" },
  { id: "info", label: "Info" },
  { id: "acknowledged", label: "Acknowledged" },
  { id: "snoozed", label: "Snoozed" },
  { id: "resolved", label: "Resolved" },
] as const;

export function StateBadge({ state }: { state: string }) {
  const key = (state ?? 'open').toLowerCase();
  const meta = STATE_BADGES[key] ?? STATE_BADGES.open;
  return (
    <span
      role="status"
      aria-label={`State: ${meta.label}`}
      className={
        'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
        meta.classes
      }
    >
      {meta.label}
    </span>
  );
}

export function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '—';
  return t.toLocaleString();
}

export function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

// Compute per-tab counts from the full alert list.
export function computeAlertCounts(
  alerts: { severity?: string; state?: string }[],
  keys: string[]
): Record<string, number> {
  const c: Record<string, number> = {};
  for (const k of keys) c[k] = 0;
  for (const a of alerts) {
    const sev = (a.severity ?? '').toLowerCase();
    const st = (a.state ?? '').toLowerCase();
    if (sev === 'critical' || sev === 'emergency') c.critical += 1;
    if (sev === 'warning') c.warning += 1;
    if (sev === 'info') c.info += 1;
    if (st === 'acknowledged') c.acknowledged += 1;
    if (st === 'snoozed') c.snoozed += 1;
    if (st === 'resolved' || st === 'closed') c.resolved += 1;
  }
  return c;
}
