// WebSocket subscription for patches — extracted from usePatches for file-size compliance.

import { useEffect, type RefObject } from 'react';
import { getWsClient, type WsEnvelope, type Status } from './websocket';
import {
  type PatchJob,
  type PatchJobStatus,
  type DeploymentStage,
  type PatchTarget,
  type PatchReboot,
  type PatchScanResult,
} from './usePatches_types';
import { isPatchEvent } from './usePatches_types';

function dedupStages(stages: DeploymentStage[]): DeploymentStage[] {
  const seen = new Set<DeploymentStage>();
  const out: DeploymentStage[] = [];
  for (const s of stages) {
    if (!seen.has(s)) { seen.add(s); out.push(s); }
  }
  return out;
}

export function usePatchWebSocket(
  mountedRef: RefObject<boolean>,
  setJobs: React.Dispatch<React.SetStateAction<PatchJob[]>>,
  setStatus: React.Dispatch<React.SetStateAction<Status>>,
  setScans: React.Dispatch<React.SetStateAction<PatchScanResult[]>>,
) {
  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());
    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const handler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isPatchEvent(env)) return;
      const payload = env.data as unknown;

      if (env.event === 'patch.job.created') {
        const j = payload as PatchJob;
        setJobs((prev) => {
          if (prev.some((x) => x.id === j.id)) return prev;
          return [j, ...prev];
        });
        return;
      }

      if (env.event === 'patch.job.updated') {
        const j = payload as PatchJob;
        setJobs((prev) => {
          const idx = prev.findIndex((x) => x.id === j.id);
          if (idx === -1) return [j, ...prev];
          const next = prev.slice();
          next[idx] = { ...next[idx], ...j };
          return next;
        });
        return;
      }

      if (env.event === 'patch.job.state') {
        const s = payload as {
          id: string;
          status: PatchJobStatus;
          stage?: DeploymentStage;
          timestamp?: string;
          previous_status?: string;
        };
        setJobs((prev) =>
          prev.map((j) =>
            j.id === s.id
              ? {
                  ...j,
                  status: s.status,
                  updated_at: s.timestamp ?? j.updated_at,
                  ...(s.stage ? { stages: dedupStages([...(j.stages ?? []), s.stage]) } : {}),
                }
              : j
          )
        );
        return;
      }

      if (env.event === 'patch.target.updated') {
        const t = payload as PatchTarget;
        setJobs((prev) =>
          prev.map((j) => {
            if (j.id !== t.job_id) return j;
            const existing = j.targets ?? [];
            const idx = existing.findIndex((x) => x.id === t.id);
            const nextTargets =
              idx === -1 ? [...existing, t] : existing.map((x) => (x.id === t.id ? { ...x, ...t } : x));
            return { ...j, targets: nextTargets };
          })
        );
        return;
      }

      if (env.event === 'patch.reboot') {
        const r = payload as PatchReboot;
        setJobs((prev) =>
          prev.map((j) => {
            if (j.id !== r.job_id) return j;
            const existing = j.reboots ?? [];
            const idx = existing.findIndex((x) => x.id === r.id);
            const nextReboots =
              idx === -1
                ? [...existing, r]
                : existing.map((x) => (x.id === r.id ? { ...x, ...r } : x));
            return { ...j, reboots: nextReboots };
          })
        );
        return;
      }

      if (env.event === 'patch.scan.completed') {
        const s = payload as PatchScanResult;
        setScans((prev) => {
          if (prev.some((x) => x.id === s.id)) return prev;
          return [s, ...prev].slice(0, 500);
        });
        return;
      }
    };

    const unsub = ws.subscribe('patches', handler);
    return () => {
      clearInterval(statusInterval);
      unsub();
    };
  }, []);
}
