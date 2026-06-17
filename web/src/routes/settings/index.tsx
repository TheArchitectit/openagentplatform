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
import './settings.css';

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
    <div className="settings-layout">
      <nav className="settings-sidebar" aria-label="Settings navigation">
        <div className="settings-sidebar-title" role="heading" aria-level={1}>Settings</div>
        {subNav.map((item) => {
          const isActive =
            item.to === '/settings'
              ? currentPath === '/settings'
              : currentPath.startsWith(item.to);
          const Icon = item.icon;
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={isActive ? 'page' : undefined}
              className={`settings-nav-link ${isActive ? 'active' : ''}`}
            >
              <Icon aria-hidden="true" />
              <span>{item.label}</span>
            </Link>
          );
        })}
      </nav>

      <div className="settings-content" role="region" aria-label="Settings content">
        {currentPath === '/settings' ? <OrganizationView /> : <Outlet />}
      </div>
    </div>
  );
}

function OrganizationView() {
  return (
    <>
      <div className="settings-page-header">
        <div>
          <h1>Organization</h1>
          <p>Manage your organization profile and global preferences.</p>
        </div>
      </div>

      <div className="settings-card">
        <h2 className="settings-card-title">Organization Details</h2>
        <p className="settings-card-desc">
          Your organization name and default contact information.
        </p>
        <div className="settings-form-group">
          <label className="settings-form-label" htmlFor="org-name">
            Organization Name
          </label>
          <input
            id="org-name"
            type="text"
            className="settings-input"
            defaultValue="OpenAgentPlatform"
            readOnly
          />
        </div>
        <div className="settings-form-group">
          <label className="settings-form-label" htmlFor="org-slug">
            Slug
          </label>
          <input
            id="org-slug"
            type="text"
            className="settings-input"
            defaultValue="openagentplatform"
            readOnly
          />
        </div>
      </div>

      <div className="settings-card">
        <h2 className="settings-card-title">Defaults</h2>
        <p className="settings-card-desc">
          Default time zone, locale, and session timeout for the organization.
        </p>
        <div className="settings-form-group">
          <label className="settings-form-label" htmlFor="org-tz">
            Default Time Zone
          </label>
          <select id="org-tz" className="settings-select" defaultValue="UTC">
            <option value="UTC">UTC</option>
            <option value="America/New_York">America/New_York</option>
            <option value="America/Los_Angeles">America/Los_Angeles</option>
            <option value="Europe/London">Europe/London</option>
            <option value="Europe/Berlin">Europe/Berlin</option>
            <option value="Asia/Tokyo">Asia/Tokyo</option>
          </select>
        </div>
        <div className="settings-form-group">
          <label className="settings-form-label" htmlFor="org-session">
            Session Timeout (minutes)
          </label>
          <input
            id="org-session"
            type="number"
            className="settings-input"
            defaultValue={60}
            min={5}
            max={1440}
          />
        </div>
      </div>
    </>
  );
}
