// Patch job detail page.
//
// Layout:
//   • Header: job name, severity, status badge, creator, key timestamps.
//   • Action bar: Approve / Reject / Cancel / Rollback / Retry Failed.
//   • Approval section: approval history with decision, approver, note, time.
//   • Deployment progress: staged rollout visualization (10% → 25% → 50% → 100%).
//   • Target agents table: hostname, current/target versions, install + reboot
//     status, schedule reboot / reboot now inline actions.
//   • Reboot coordination panel: pending reboots with staggered timeline view.
//   • Real-time WebSocket merge of job updates, target updates, and reboots.

import { createFileRoute, Link } from '@tanstack/react-router';
import { ArrowLeft, Loader2 } from 'lucide-react';
import { usePatchJobDetail } from './usePatchJobDetail';
import { PatchJobDetailView } from './PatchJobDetailView';

export const Route = createFileRoute('/patches/$jobId')({
  component: PatchJobDetailPage,
});

function PatchJobDetailPage() {
  const { jobId } = Route.useParams();
  const {
    job,
    targets,
    approvals,
    reboots,
    error,
    isLoading,
    actionBusy,
    scheduleOpen,
    scheduleValue,
    progress,
    activeStageIdx,
    targetsByStage,
    isTerminal,
    doAction,
    doRebootNow,
    doScheduleReboot,
    setScheduleOpen,
    setScheduleValue,
  } = usePatchJobDetail(jobId);

  if (isLoading && !job) {
    return (
      <div className="text-center text-gray-400 py-24">
        <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
        Loading patch job…
      </div>
    );
  }

  if (error && !job) {
    return (
      <div className="space-y-4">
        <Link
          to="/patches"
          className="inline-flex items-center gap-2 text-sm text-gray-300 hover:text-white"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to patches</span>
        </Link>
        <div className="rounded-lg border border-red-800 bg-red-500/5 p-6 text-red-400">
          Failed to load job: {error.message}
        </div>
      </div>
    );
  }

  if (!job) return null;

  const state = (job.status ?? 'pending_approval').toLowerCase() as NonNullable<
    typeof job.status
  >;

  return (
    <PatchJobDetailView
      job={job}
      state={state}
      progress={progress}
      activeStageIdx={activeStageIdx}
      targetsByStage={targetsByStage}
      targets={targets}
      approvals={approvals}
      reboots={reboots}
      actionBusy={actionBusy}
      scheduleOpen={scheduleOpen}
      scheduleValue={scheduleValue}
      doAction={doAction}
      doRebootNow={doRebootNow}
      setScheduleOpen={setScheduleOpen}
      setScheduleValue={setScheduleValue}
      doScheduleReboot={doScheduleReboot}
      isTerminal={isTerminal}
    />
  );
}
