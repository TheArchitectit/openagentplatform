// format — reusable display formatters for bytes, durations, percentages,
// and numbers. Consolidates inline formatting helpers spread across routes.

// ---------------------------------------------------------------------------
// Bytes
// ---------------------------------------------------------------------------

/** Format a byte count into human-readable size string. */
export function fmtBytes(n: number): string {
  if (n < 0) return `−${fmtBytes(-n)}`;
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'] as const;
  let val = n / 1024;
  let unitIdx = 0;
  while (val >= 1024 && unitIdx < units.length - 1) {
    val /= 1024;
    unitIdx++;
  }
  const decimals = val < 10 ? 2 : 1;
  return `${val.toFixed(decimals)} ${units[unitIdx]}`;
}

// ---------------------------------------------------------------------------
// Durations (milliseconds)
// ---------------------------------------------------------------------------
/** Format milliseconds as a compact human-readable duration. */
export function fmtMs(ms: number): string {
  if (ms < 0) return '—';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  if (m < 60) return `${m}m ${rs}s`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return `${h}h ${rm}m`;
}

// ---------------------------------------------------------------------------
// Seconds (bare)
// ---------------------------------------------------------------------------
/** Format seconds as a compact human-readable duration. */
export function fmtSec(sec: number): string {
  return fmtMs(sec * 1000);
}

// ---------------------------------------------------------------------------
// Percentage
// ---------------------------------------------------------------------------
/** Format a 0-1 fraction or 0-100 number as a percentage string. */
export function fmtPct(value: number, options?: { decimals?: number }): string {
  const d = options?.decimals ?? 1;
  // If value <= 1 the caller is likely passing a fraction (e.g. 0.78 => 78%).
  const v = value <= 1 ? value * 100 : value;
  return `${v.toFixed(d)}%`;
}

// ---------------------------------------------------------------------------
// Number
// ---------------------------------------------------------------------------
/** Format an integer with locale-aware thousands separators and an optional suffix. */
export function fmtNum(n: number): string {
  return n.toLocaleString('en-US');
}

// ---------------------------------------------------------------------------
// Default export for convenience importing
// ---------------------------------------------------------------------------
export default { fmtBytes, fmtMs, fmtSec, fmtPct, fmtNum };