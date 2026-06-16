export const meta = {
  name: 'sprint-14-impl',
  description: 'Implement Sprint 1.4: Patch approval workflow, inventory/scan, deployment engine, status UI',
  phases: [
    { title: 'Approval', detail: 'Patch approval workflow with RBAC' },
    { title: 'Inventory', detail: 'Patch inventory and scan engine' },
    { title: 'Deploy', detail: 'Patch deployment engine' },
    { title: 'Status', detail: 'Patch status UI with reboot coordination' },
    { title: 'Commit', detail: 'Stage, commit, push, close Sprint 1.4 issues' },
  ],
}

log('Sprint 1.4 implementation starting -- Patches')

var repoRoot = '/mnt/data/git/openagentplatform'

// Phase 1: Patch approval workflow (sonnet -- RBAC + state machine)
phase('Approval')
var approval = await agent(
  'Build the patch approval workflow with RBAC at ' + repoRoot + '.\n' +
  'Read existing internal/alerts/statemachine.go (for pattern), internal/api/routes.go, pkg/models/models.go first.\n' +
  '\n' +
  '**1. Create internal/patches/approval.go** -- ApprovalWorkflow:\n' +
  '- Patch job states: pending_approval, approved, rejected, scheduled, in_progress, completed, failed, rolled_back\n' +
  '- ValidTransitions map:\n' +
  '  * pending_approval -> approved (by approver), rejected (by approver)\n' +
  '  * approved -> scheduled (by admin), rejected (by admin override)\n' +
  '  * scheduled -> in_progress (auto on schedule time), cancelled (by admin)\n' +
  '  * in_progress -> completed (auto on success), failed (auto on failure)\n' +
  '  * failed -> pending_approval (retry), rolled_back (auto/manual)\n' +
  '  * completed -> rolled_back (manual)\n' +
  '- Approval rules: critical patches auto-approve with notification; standard require 1 approver; major OS upgrades require 2 approvers; 72h approval timeout auto-approve\n' +
  '- RBAC: patch:approve, patch:reject, patch:schedule, patch:cancel, patch:rollback permissions\n' +
  '- Approval audit: who approved/rejected, when, with optional comment\n' +
  '- Maintenance windows: patches scheduled within maintenance_window_start/end\n' +
  '\n' +
  '**2. Create internal/patches/store.go** -- PostgreSQL PatchJobStore with full CRUD, filters, stats\n' +
  '\n' +
  '**3. Create internal/api/patches.go** -- Patch API:\n' +
  '- GET /api/v1/patches, GET /api/v1/patches/{id}\n' +
  '- POST /api/v1/patches/jobs -- create patch job\n' +
  '- POST /api/v1/patches/{id}/approve, /reject, /schedule, /cancel, /rollback\n' +
  '- GET /api/v1/patches/stats\n' +
  '\n' +
  '**4. Update internal/api/routes.go** -- register patch routes\n' +
  '**5. Update pkg/models/models.go** -- PatchJob, PatchJobTarget, ApprovalRecord structs\n' +
  '**6. Update internal/api/handler.go** -- add patchStore setter\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'patch-approval', phase: 'Approval', model: 'sonnet' }
)

// Phase 2: Patch inventory and scan engine (sonnet -- agent + server)
phase('Inventory')
var inventory = await agent(
  'Build the patch inventory and scan engine at ' + repoRoot + '.\n' +
  'Read existing pkg/agent/checkers/ (for pattern), cmd/agent/main.go first.\n' +
  '\n' +
  '**1. Create pkg/agent/patcher/patcher.go** -- Agent-side patch scanner:\n' +
  '- PatchScanner interface: Scan(ctx) -> []PatchInfo, error\n' +
  '- WindowsScanner: wmic qfe, Get-HotFix, winget list --upgradable\n' +
  '- LinuxScanner: detects pkg mgr (apt/dnf/yum/zypper/apk/pacman) and uses appropriate check-update command\n' +
  '- macOSScanner: softwareupdate -l, brew outdated\n' +
  '- Auto-detect OS and select scanner\n' +
  '- PatchInfo struct: name, installed_version, available_version, severity, category, cve_ids[], kb_id, package_manager, size_bytes, reboot_required\n' +
  '\n' +
  '**2. Create pkg/agent/patcher/installer.go** -- Agent-side installer:\n' +
  '- PatchInstaller interface: Install(patch) -> InstallResult, error\n' +
  '- Windows: wusa.exe /quiet /norestart (msu), msiexec /qn /norestart (msi), winget upgrade\n' +
  '- Linux (per pkg mgr): apt install -y, dnf upgrade -y, etc.\n' +
  '- macOS: softwareupdate -i, brew upgrade\n' +
  '- InstallResult: success, output, reboot_required, error_message\n' +
  '- Rollback support per platform\n' +
  '\n' +
  '**3. Create internal/patches/scanner.go** -- Server-side scan orchestration:\n' +
  '- PatchScanDispatcher: publish to oap.agents.<id>.patch_scan, subscribe to oap.agents.*.patch_scan.results\n' +
  '- Scheduled scan: every X hours scan all agents\n' +
  '- On-demand scan: per agent or site\n' +
  '- PatchCatalog: aggregate across all agents, dedup by name+arch\n' +
  '\n' +
  '**4. Update cmd/agent/main.go** -- register patch scan + install handlers\n' +
  '\n' +
  '**5. Create internal/api/patch_catalog.go** -- Catalog API\n' +
  '**6. Update internal/api/routes.go** -- register catalog routes\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'patch-inventory', phase: 'Inventory', model: 'sonnet' }
)

// Phase 3: Patch deployment engine (sonnet -- orchestration)
phase('Deploy')
var deploy = await agent(
  'Build the patch deployment engine at ' + repoRoot + '.\n' +
  'Read existing internal/patches/approval.go, internal/patches/store.go, internal/patches/scanner.go first.\n' +
  '\n' +
  '**1. Create internal/patches/deployer.go** -- PatchDeployer:\n' +
  '- DeploymentStrategy interface: Deploy(job, targets) -> DeployResult\n' +
  '- StagedRollout: 10% -> wait -> 25% -> wait -> 50% -> wait -> 100%\n' +
  '  * Configurable stage_sizes[], success_threshold (95%), wait_duration (15min)\n' +
  '- CanaryDeploy: deploy to 1 agent, verify, then rest\n' +
  '- AllAtOnce: deploy to all targets simultaneously\n' +
  '- Per-target: check online, publish install to oap.agents.<id>.patch_install, wait for result with timeout\n' +
  '- On failure: retry (max_retries), abort if failure_rate exceeds threshold\n' +
  '- Post-install verify: trigger patch scan, run health checks, fail+rollback if verify fails\n' +
  '- Reboot coordination: queue reboots for maintenance window, staggered sequence, pre/post-reboot health checks\n' +
  '\n' +
  '**2. Create internal/patches/scheduler.go** -- PatchScheduler:\n' +
  '- Schedule at specific time or during maintenance window\n' +
  '- Conflict detection (no simultaneous agent deploys)\n' +
  '- Priority queue (critical first), max concurrency (default 10)\n' +
  '- Blackout periods support\n' +
  '\n' +
  '**3. Update cmd/server/main.go** -- init PatchDeployer + PatchScheduler, graceful shutdown\n' +
  '\n' +
  'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
  'Report back files created and any errors.',
  { label: 'patch-deploy', phase: 'Deploy', model: 'sonnet' }
)

// Phase 4: Patch status UI with reboot coordination (sonnet -- React)
phase('Status')
var status = await agent(
  'Build the Patch status UI with reboot coordination at ' + repoRoot + '.\n' +
  'Read existing web/src/routes/, web/src/components/, web/src/lib/ first. DO NOT run pnpm/npm install.\n' +
  '\n' +
  '**1. Create web/src/routes/patches/index.tsx** -- Patch management page:\n' +
  '- Summary bar: Total, Critical, Security, Approved, In Progress, Completed Today\n' +
  '- Filter tabs: All, Pending Approval, Approved, In Progress, Completed, Failed\n' +
  '- Table: Patch Name, KB/CVE, Severity badge, Affected Agents, Status, Progress bar, Actions\n' +
  '- Create Job button: multi-step modal (select patches -> select targets -> configure -> review+submit)\n' +
  '- Batch approve/reject\n' +
  '\n' +
  '**2. Create web/src/routes/patches/$jobId.tsx** -- Patch job detail:\n' +
  '- Job header: patch names, status, severity, creator, timeline\n' +
  '- Approval section with approve/reject buttons and history\n' +
  '- Deployment progress: staged rollout visualization (10%->25%->50%->100%)\n' +
  '- Target agents table: hostname, versions, install status, reboot status\n' +
  '- Reboot coordination panel: pending reboot agent list, reboot now/schedule, staggered timeline view\n' +
  '- Actions: Cancel, Rollback, Retry Failed\n' +
  '\n' +
  '**3. Create web/src/lib/usePatches.ts** -- React hook with patch jobs, catalog, scan, WebSocket\n' +
  '**4. Update web/src/components/sidebar.tsx** -- ensure Patches nav link\n' +
  '**5. Update web/src/routes/dashboard.tsx** -- add patch KPI cards\n' +
  '\n' +
  'Report back files created/modified.',
  { label: 'patch-status', phase: 'Status', model: 'sonnet' }
)

// Phase 5: Commit, push, close issues (haiku -- mechanical)
phase('Commit')
var commit = await agent(
  'Stage, commit, and push Sprint 1.4 implementation from ' + repoRoot + '.\n' +
  '\n' +
  'Run:\n' +
  '1. cd ' + repoRoot + '\n' +
  '2. git status\n' +
  '3. git add -A\n' +
  '4. git commit -m "Sprint 1.4: Patch approval workflow, inventory scan, deployment engine, status UI" -m "- Patch approval workflow with RBAC, multi-stage state machine, maintenance windows" -m "- Patch inventory scanner: Windows/Linux/macOS with auto-detect" -m "- Patch deployment engine: staged rollout, canary, all-at-once strategies" -m "- Patch management UI with job wizard, progress visualization, reboot coordination" -m "Closes #25, #26, #28, #31"\n' +
  '5. git push origin main\n' +
  '6. for i in 25 26 28 31; do gh issue close $i -r completed; done\n' +
  '\n' +
  'If commit signing fails, retry with: git -c commit.gpgsign=false commit ...\n' +
  'Report the commit SHA and confirmation.',
  { label: 'commit-push', phase: 'Commit', model: 'haiku' }
)

return {
  status: 'Sprint 1.4 complete',
  phases: { approval: approval, inventory: inventory, deploy: deploy, status: status, commit: commit },
}
