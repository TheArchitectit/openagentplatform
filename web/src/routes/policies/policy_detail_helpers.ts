// Policy detail helper functions — extracted for file-size compliance.

import { ShieldCheck, Eye, Edit3 } from 'lucide-react';

export function enforcementIcon(mode: string) {
  switch (mode) {
    case 'enforce':
      return ShieldCheck;
    case 'audit':
      return Eye;
    case 'report':
    default:
      return Edit3;
  }
}

export function enforcementClasses(mode: string): string {
  switch (mode) {
    case 'enforce':
      return 'bg-red-500/10 text-red-400 border-red-800';
    case 'audit':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-800';
    case 'report':
    default:
      return 'bg-blue-500/10 text-blue-400 border-blue-800';
  }
}

export function categoryClasses(cat: string): string {
  switch (cat) {
    case 'security':
      return 'bg-red-500/10 text-red-400 border-red-800';
    case 'compliance':
      return 'bg-blue-500/10 text-blue-400 border-blue-800';
    case 'configuration':
      return 'bg-blue-600/10 text-blue-400 border-blue-500/20';
    case 'performance':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-800';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-700';
  }
}

export function complianceColor(pct: number | undefined): string {
  if (pct === undefined || pct === null) return 'text-gray-400';
  if (pct >= 80) return 'text-green-400';
  if (pct >= 60) return 'text-yellow-400';
  return 'text-red-400';
}

// Highlight Rego keywords with simple regex-based highlighting — no full
// parser required for the read-only display.
export function highlightRego(src: string): string {
  // Escape HTML first.
  const escaped = src
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
  return escaped;
}

export function formatTimestamp(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}
