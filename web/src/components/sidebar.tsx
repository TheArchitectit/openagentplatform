// Sidebar — primary left-side navigation.
//
// The "Alerts" entry shows a live badge with the number of open critical
// alerts so operators can see what needs attention at a glance, even
// when they are on another section of the app.

import { Link } from '@tanstack/react-router';
import {
  LayoutDashboard,
  Bot,
  Activity,
  BellRing,
  ShieldCheck,
  Wrench,
  FileCode2,
  Settings,
  LogOut,
  Terminal,
  Radio,
  ListChecks,
  CircleDollarSign,
  Network,
} from 'lucide-react';
import { logout, getStoredUser } from '@/lib/auth';
import { useAlerts } from '@/lib/useAlerts';
import { usePatches } from '@/lib/usePatches';

interface NavItem {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
  showAlertBadge?: boolean;
  showPatchBadge?: boolean;
}

const navItems: NavItem[] = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/agents', label: 'Agents', icon: Bot },
  { to: '/checks', label: 'Checks', icon: Activity },
  { to: '/alerts', label: 'Alerts', icon: BellRing, showAlertBadge: true },
  { to: '/policies', label: 'Policies', icon: ShieldCheck },
  { to: '/patches', label: 'Patches', icon: Wrench, showPatchBadge: true },
  { to: '/scripts', label: 'Scripts', icon: FileCode2 },
  { to: '/a2a', label: 'A2A Dashboard', icon: Radio },
  { to: '/a2a/agents', label: 'Agent Cards', icon: Network },
  { to: '/a2a/tasks', label: 'A2A Tasks', icon: ListChecks },
  { to: '/a2a/costs', label: 'A2A Costs', icon: CircleDollarSign },
  { to: '/settings', label: 'Settings', icon: Settings },
  // Admin-only items are appended dynamically below; we keep the
  // static list above so the dashboard link renders for everyone.
];

// Admin-only entries appended after construction. Keeping them in
// a second list lets us reuse the same rendering path.
const adminNavItems: NavItem[] = [
  { to: '/shell-recordings', label: 'Shell Recordings', icon: Terminal },
];

export function Sidebar() {
  const user = getStoredUser();
  const initials = user?.name
    ? user.name.split(' ').map((p) => p.charAt(0).toUpperCase()).slice(0, 2).join('')
    : user?.email?.charAt(0).toUpperCase() ?? 'U';
  // Surface admin-only nav entries (shell recordings, etc.) to
  // operators with elevated roles. The list page itself does its
  // own role check via the API filter so this is purely a UX hint.
  const isAdminUser =
    user?.role === 'admin' || user?.role === 'owner' || user?.role === 'superadmin';

  // Subscribe to the "alerts" channel via the dedicated "all" filter so
  // we can render a count badge for the currently-open critical alerts.
  // The list itself is not consumed here — only the count is needed.
  const { alerts } = useAlerts('all');
  const openCriticalCount = alerts.filter(
    (a) =>
      (a.severity === 'critical' || a.severity === 'emergency') &&
      (a.state === 'open' || a.state === undefined)
  ).length;

  // Patch jobs awaiting approval — drives the sidebar badge so operators
  // can see pending approvals at a glance.
  const { jobs: patchJobs } = usePatches();
  const pendingPatchCount = patchJobs.filter(
    (j) => j.status === 'pending_approval'
  ).length;

  return (
    <aside className="w-60 shrink-0 bg-slate-900 border-r border-slate-800 flex flex-col">
      {/* Logo */}
      <div className="h-14 px-5 flex items-center border-b border-slate-800">
        <div className="h-7 w-7 rounded-md bg-indigo-600 flex items-center justify-center mr-2">
          <ShieldCheck className="h-4 w-4 text-white" />
        </div>
        <span className="font-semibold text-slate-100">OAP</span>
      </div>

      {/* Nav */}
      <nav className="flex-1 p-3 space-y-1 overflow-y-auto">
        {[...navItems, ...(isAdminUser ? adminNavItems : [])].map((item) => {
          const Icon = item.icon;
          const isAlerts = item.showAlertBadge === true;
          const isPatches = item.showPatchBadge === true;
          return (
            <Link
              key={item.to}
              to={item.to}
              className="flex items-center gap-3 px-3 py-2 rounded-md text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
              activeProps={{
                className:
                  'flex items-center gap-3 px-3 py-2 rounded-md text-sm bg-slate-800 text-white',
              }}
            >
              <Icon size={16} />
              <span className="flex-1">{item.label}</span>
              {isAlerts && openCriticalCount > 0 && (
                <span
                  className="inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1.5 rounded-full bg-rose-500/20 border border-rose-500/30 text-[10px] font-semibold text-rose-300"
                  title={`${openCriticalCount} open critical alert${openCriticalCount === 1 ? '' : 's'}`}
                >
                  {openCriticalCount > 99 ? '99+' : openCriticalCount}
                </span>
              )}
              {isPatches && pendingPatchCount > 0 && (
                <span
                  className="inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1.5 rounded-full bg-amber-500/20 border border-amber-500/30 text-[10px] font-semibold text-amber-300"
                  title={`${pendingPatchCount} patch job${pendingPatchCount === 1 ? '' : 's'} awaiting approval`}
                >
                  {pendingPatchCount > 99 ? '99+' : pendingPatchCount}
                </span>
              )}
            </Link>
          );
        })}
      </nav>

      {/* User */}
      <div className="p-3 border-t border-slate-800">
        <div className="flex items-center gap-3 px-2 py-2 rounded-md">
          <div className="h-8 w-8 rounded-full bg-indigo-600 flex items-center justify-center text-sm font-medium text-white shrink-0">
            {initials}
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm text-slate-200 truncate">{user?.name ?? user?.email ?? 'User'}</p>
            <p className="text-xs text-slate-500 truncate">{user?.email ?? ''}</p>
          </div>
          <button
            type="button"
            onClick={logout}
            title="Sign out"
            className="p-1.5 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
          >
            <LogOut size={16} />
          </button>
        </div>
      </div>
    </aside>
  );
}
