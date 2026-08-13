// Additional KPI aggregation helpers for useDashboard, split out of
// useDashboard.helpers.ts to keep that file under the size gate. These are
// re-exported from useDashboard.helpers.ts so importers need no changes.

import {
  Wrench,
  Shield,
  CircleCheck,
  CirclePlay,
  FileCode2,
  CircleAlert,
  Timer,
} from 'lucide-react';
import type { PatchJob } from '@/lib/usePatches';
import type { Script } from '@/lib/useScripts';
import type { Kpi } from './dashboard_components';
import { isToday } from './dashboard_components';

export function computePatchKpis(patchJobs: PatchJob[], patchesLoading: boolean): Kpi[] {
  let total = 0;
  let critical = 0;
  let security = 0;
  let approved = 0;
  let inProgress = 0;
  let completedToday = 0;
  for (const j of patchJobs) {
    total += 1;
    const sev = (j.severity ?? '').toLowerCase();
    if (sev === 'critical' || sev === 'emergency') critical += 1;
    if (sev === 'important' || j.patch_count > 0) security += 1;
    if (j.status === 'approved') approved += 1;
    if (j.status === 'in_progress') inProgress += 1;
    if (j.status === 'completed' && isToday(j.completed_at)) completedToday += 1;
  }
  const dash = patchesLoading && patchJobs.length === 0 ? '—' : null;
  return [
    {
      label: 'Total Jobs',
      value: dash ?? String(total),
      delta: total === 0 ? 'No jobs yet' : `${total} tracked`,
      deltaTone: 'neutral',
      icon: Wrench,
      to: '/patches',
    },
    {
      label: 'Critical',
      value: dash ?? String(critical),
      delta: critical === 0 ? 'No critical' : 'Action required',
      deltaTone: critical === 0 ? 'neutral' : 'down',
      icon: Shield,
      to: '/patches',
    },
    {
      label: 'Approved',
      value: dash ?? String(approved),
      delta: approved === 0 ? 'None queued' : 'Ready to deploy',
      deltaTone: 'neutral',
      icon: CircleCheck,
      to: '/patches',
    },
    {
      label: 'In Progress',
      value: dash ?? String(inProgress),
      delta: inProgress === 0 ? 'Idle' : 'Rolling out',
      deltaTone: inProgress === 0 ? 'neutral' : 'up',
      icon: CirclePlay,
      to: '/patches',
    },
  ];
}

export function computeScriptKpis(
  scripts: Script[],
  scriptsTotal: number,
  scriptsLoading: boolean
): Kpi[] {
  const total = scriptsTotal || scripts.length;
  let succeeded = 0;
  let failed = 0;
  let running = 0;
  let totalRuns = 0;
  for (const s of scripts) {
    if (typeof s.run_count === 'number') totalRuns += s.run_count;
    if (s.last_status === 'completed') succeeded += 1;
    if (s.last_status === 'failed' || s.last_status === 'timeout') failed += 1;
    if (s.last_status === 'in_progress' || s.last_status === 'pending') running += 1;
  }
  const dash = scriptsLoading && scripts.length === 0 ? '—' : null;
  return [
    {
      label: 'Total Scripts',
      value: dash ?? String(total),
      delta: total === 0 ? 'No scripts yet' : `${total} in library`,
      deltaTone: 'neutral',
      icon: FileCode2,
      to: '/scripts',
    },
    {
      label: 'Last Run OK',
      value: dash ?? String(succeeded),
      delta: succeeded === 0 ? 'No clean runs' : 'Most recent succeeded',
      deltaTone: succeeded === 0 ? 'neutral' : 'up',
      icon: CircleCheck,
      to: '/scripts',
    },
    {
      label: 'Last Run Failed',
      value: dash ?? String(failed),
      delta: failed === 0 ? 'No failures' : 'Investigate runs',
      deltaTone: failed === 0 ? 'neutral' : 'down',
      icon: CircleAlert,
      to: '/scripts',
    },
    {
      label: 'Total Runs',
      value: dash ?? String(totalRuns),
      delta: totalRuns === 0 ? 'No runs yet' : `${running} active`,
      deltaTone: running > 0 ? 'up' : 'neutral',
      icon: Timer,
      to: '/scripts',
    },
  ];
}
