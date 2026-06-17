// SeverityBadge — a reusable color + icon badge for alert severities.
//
// Severities map to standard monitoring conventions:
//   info      — blue,   circle-info
//   warning   — yellow, triangle-exclamation
//   critical  — red,    circle-x
//   emergency — red,    fire
//
// The component is intentionally a pure presentational primitive so it
// can be used in tables, detail pages, KPIs, and timeline rows alike.

import { Info, TriangleAlert, CircleX, Flame } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export type Severity = 'info' | 'warning' | 'critical' | 'emergency';

export interface SeverityMeta {
  label: string;
  classes: string;
  icon: LucideIcon;
}

export const SEVERITY_META: Record<Severity, SeverityMeta> = {
  info: {
    label: 'Info',
    classes: 'bg-info/10 text-info border-info/20',
    icon: Info,
  },
  warning: {
    label: 'Warning',
    classes: 'bg-warning/10 text-warning border-warning/20',
    icon: TriangleAlert,
  },
  critical: {
    label: 'Critical',
    classes: 'bg-danger/10 text-danger border-danger/20',
    icon: CircleX,
  },
  emergency: {
    label: 'Emergency',
    classes: 'bg-danger/15 text-danger border-danger/30',
    icon: Flame,
  },
};

function normalizeSeverity(value: string | undefined | null): Severity {
  const v = (value ?? '').toLowerCase();
  if (v === 'emergency' || v === 'emerg') return 'emergency';
  if (v === 'critical' || v === 'crit') return 'critical';
  if (v === 'warning' || v === 'warn') return 'warning';
  return 'info';
}

export interface SeverityBadgeProps {
  severity: string | Severity;
  size?: 'sm' | 'md';
  showIcon?: boolean;
  showLabel?: boolean;
  title?: string;
}

export function SeverityBadge({
  severity,
  size = 'sm',
  showIcon = true,
  showLabel = true,
  title,
}: SeverityBadgeProps) {
  const meta = SEVERITY_META[normalizeSeverity(severity)];
  const Icon = meta.icon;
  const sizing =
    size === 'md'
      ? 'px-2.5 py-1 text-sm gap-1.5'
      : 'px-2 py-0.5 text-xs gap-1';

  return (
    <span
      role="status"
      aria-label={title ?? `Severity: ${meta.label}`}
      className={
        'inline-flex items-center rounded-full border font-medium ' +
        sizing +
        ' ' +
        meta.classes
      }
    >
      {showIcon && <Icon className={size === 'md' ? 'h-3.5 w-3.5' : 'h-3 w-3'} aria-hidden="true" />}
      {showLabel && <span>{meta.label}</span>}
    </span>
  );
}

export default SeverityBadge;
