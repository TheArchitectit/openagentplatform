// Settings landing — org view with left sub-navigation.
//
// The sub-navigation drives the rest of the settings section. The default
// landing view is "Organization" (this page) which shows org-level info.

import { createFileRoute, Link, Outlet, useLocation } from '@tanstack/react-router';
import {
  Building2,
  Users,
  ShieldCheck,
  KeyRound,
  Lock,
  ScrollText,
} from 'lucide-react';

export const Route = createFileRoute('/settings/')({
  component: SettingsLayout,
});

interface SubNav {
  to: string;
  label: string;
  icon: typeof Building2;
}

const subNav: SubNav[] = [
  { to: '/settings', label: 'Organization', icon: Building2 },
  { to: '/settings/users', label: 'Users', icon: Users },
  { to: '/settings/roles', label: 'Roles', icon: ShieldCheck },
  { to: '/settings/api-keys', label: 'API Keys', icon: KeyRound },
  { to: '/settings/sso', label: 'SSO', icon: Lock },
  { to: '/settings/audit-log', label: 'Audit Log', icon: ScrollText },
];

function SettingsLayout() {
  const location = useLocation();
  const currentPath = location.pathname.replace(/\/$/, '') || '/settings';

  return (
    <div className="flex gap-6 min-h-[calc(100vh-8rem)]">
      {/* Sidebar nav */}
      <aside
        className="w-56 flex-shrink-0 flex flex-col gap-0.5 p-3 rounded-lg bg-slate-900 border border-slate-800 h-fit sticky top-6"
        aria-label="Settings sub-navigation"
      >
        <div className="text-[10px] uppercase tracking-wider text-gray-400 px-2 mb-1.5 font-semibold">
          Settings
        </div>
        {subNav.map((item) => {
          const itemPath = item.to.replace(/\/$/, '') || '/settings';
          const isActive = currentPath === itemPath;
          const Icon = item.icon;
          return (
            <Link
              key={item.to}
              to={item.to}
              className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${
                isActive
                  ? 'bg-blue-600/10 text-blue-400 border-l-2 border-blue-500'
                  : 'text-gray-300 hover:bg-slate-800 hover:text-white border-l-2 border-transparent'
              }`}
              aria-current={isActive ? 'page' : undefined}
            >
              <Icon className="h-4 w-4" />
              {item.label}
            </Link>
          );
        })}
      </aside>

      {/* Content */}
      <main className="flex-1 min-w-0">
        {currentPath === '/settings' ? <OrganizationLanding /> : <Outlet />}
      </main>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Organization landing page
// ---------------------------------------------------------------------------

function OrganizationLanding() {
  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center gap-3">
        <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
          <Building2 className="h-4 w-4 text-gray-300" />
        </div>
        <div>
          <h1 className="text-2xl font-bold text-white">Organization</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            Manage your organization's profile and subscription details.
          </p>
        </div>
      </div>

      {/* Info cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <InfoCard label="Organization Name" value="Acme Corp" />
        <InfoCard label="Plan" value="Enterprise" accent="blue" />
        <InfoCard label="Created" value={new Date().toLocaleDateString()} />
        <InfoCard label="Members" value="12 active" />
      </div>

      {/* Quick links */}
      <section className="rounded-lg border border-slate-800 bg-slate-900 p-4" aria-label="Quick links">
        <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Quick Links</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          <Link
            to="/settings/users"
            className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-800/40 px-3 py-2 text-sm text-gray-300 hover:bg-slate-800 hover:text-white hover:border-slate-700 transition-colors"
          >
            <Users className="h-4 w-4" /> Manage Users
          </Link>
          <Link
            to="/settings/roles"
            className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-800/40 px-3 py-2 text-sm text-gray-300 hover:bg-slate-800 hover:text-white hover:border-slate-700 transition-colors"
          >
            <ShieldCheck className="h-4 w-4" /> Roles &amp; Permissions
          </Link>
          <Link
            to="/settings/api-keys"
            className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-800/40 px-3 py-2 text-sm text-gray-300 hover:bg-slate-800 hover:text-white hover:border-slate-700 transition-colors"
          >
            <KeyRound className="h-4 w-4" /> API Keys
          </Link>
          <Link
            to="/settings/sso"
            className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-800/40 px-3 py-2 text-sm text-gray-300 hover:bg-slate-800 hover:text-white hover:border-slate-700 transition-colors"
          >
            <Lock className="h-4 w-4" /> SSO Configuration
          </Link>
          <Link
            to="/settings/audit-log"
            className="flex items-center gap-2 rounded-md border border-slate-800 bg-slate-800/40 px-3 py-2 text-sm text-gray-300 hover:bg-slate-800 hover:text-white hover:border-slate-700 transition-colors sm:col-span-2"
          >
            <ScrollText className="h-4 w-4" /> Audit Log
          </Link>
        </div>
      </section>
    </div>
  );
}

function InfoCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: 'blue';
}) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
      <div className="text-xs uppercase tracking-wider text-gray-300">{label}</div>
      <div className={`mt-1 text-lg font-semibold ${accent === 'blue' ? 'text-blue-400' : 'text-white'}`}>
        {value}
      </div>
    </div>
  );
}
