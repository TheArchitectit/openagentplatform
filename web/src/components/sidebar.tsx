// Sidebar — primary left-side navigation.
//
// Layout
//   * Desktop: fixed sidebar, always visible, collapsible section headers.
//   * Mobile:  slide-in drawer controlled by SidebarContext (open/close via
//              the hamburger button in the Header).  Backdrop overlay
//              dismisses on click / Escape.
//
// The "Alerts" entry shows a live badge with the number of open critical
// alerts so operators can see what needs attention at a glance, even when
// they are on another section of the app.
//
// Sections
//   Monitoring   — Dashboard, Agents, Checks, Alerts
//   Management   — Policies, Patches, Scripts, Shell Recordings
//   A2A          — Dashboard, Agent Cards, Tasks, Costs
//   Settings     — Organization, Users, Roles, API Keys, SSO, Audit Log

import { Link } from '@tanstack/react-router';
import { useCallback, useEffect, useRef, useState } from 'react';
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
  Building2,
  Users,
  KeyRound,
  Lock,
  ScrollText,
  ChevronDown,
  X,
} from 'lucide-react';
import { logout, getStoredUser } from '@/lib/auth';
import { useAlerts } from '@/lib/useAlerts';
import { usePatches } from '@/lib/usePatches';
import { useRovingTabIndex, useAriaAnnounce, AriaLiveRegion, useEscapeKey } from '@/lib/a11y';
import { useSidebar } from '@/lib/sidebar';

interface NavItem {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
  showAlertBadge?: boolean;
  showPatchBadge?: boolean;
}

interface NavSection {
  id: string;
  label: string;
  defaultExpanded?: boolean;
  items: NavItem[];
}

const NAV_SECTIONS: NavSection[] = [
  {
    id: 'monitoring',
    label: 'Monitoring',
    defaultExpanded: true,
    items: [
      { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
      { to: '/agents', label: 'Agents', icon: Bot },
      { to: '/checks', label: 'Checks', icon: Activity },
      { to: '/alerts', label: 'Alerts', icon: BellRing, showAlertBadge: true },
    ],
  },
  {
    id: 'management',
    label: 'Management',
    defaultExpanded: true,
    items: [
      { to: '/policies', label: 'Policies', icon: ShieldCheck },
      { to: '/patches', label: 'Patches', icon: Wrench, showPatchBadge: true },
      { to: '/scripts', label: 'Scripts', icon: FileCode2 },
      { to: '/shell-recordings', label: 'Shell Recordings', icon: Terminal },
    ],
  },
  {
    id: 'a2a',
    label: 'A2A',
    defaultExpanded: true,
    items: [
      { to: '/a2a', label: 'Dashboard', icon: LayoutDashboard },
      { to: '/a2a/agents', label: 'Agent Cards', icon: Radio },
      { to: '/a2a/tasks', label: 'Tasks', icon: ListChecks },
      { to: '/a2a/costs', label: 'Costs', icon: CircleDollarSign },
    ],
  },
  {
    id: 'settings',
    label: 'Settings',
    defaultExpanded: false,
    items: [
      { to: '/settings', label: 'Organization', icon: Building2 },
      { to: '/settings/users', label: 'Users', icon: Users },
      { to: '/settings/roles', label: 'Roles', icon: Network },
      { to: '/settings/api-keys', label: 'API Keys', icon: KeyRound },
      { to: '/settings/sso', label: 'SSO', icon: Lock },
      { to: '/settings/audit-log', label: 'Audit Log', icon: ScrollText },
    ],
  },
];

const COLLAPSE_KEY = 'oap-sidebar-collapsed';

function readCollapsed(): Record<string, boolean> {
  if (typeof window === 'undefined') return {};
  try {
    const raw = window.localStorage.getItem(COLLAPSE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === 'object') return parsed as Record<string, boolean>;
    return {};
  } catch {
    return {};
  }
}

function writeCollapsed(state: Record<string, boolean>) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(COLLAPSE_KEY, JSON.stringify(state));
  } catch {
    /* quota / private mode — ignore */
  }
}

export function Sidebar() {
  const user = getStoredUser();
  const { alerts } = useAlerts('critical');
  const { jobs } = usePatches();
  const criticalAlertCount = alerts.length;
  const pendingApprovalCount = jobs.filter(
    (j) => j.status === 'pending_approval'
  ).length;
  const { announce, message } = useAriaAnnounce();
  const { mobileOpen, closeMobile } = useSidebar();

  // Collapsed state per section, persisted to localStorage so the user's
  // preference survives reloads.  Defaults come from NAV_SECTIONS.
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    const stored = readCollapsed();
    const initial: Record<string, boolean> = {};
    for (const s of NAV_SECTIONS) {
      initial[s.id] = stored[s.id] ?? !s.defaultExpanded;
    }
    return initial;
  });

  // Close mobile drawer on Escape.
  useEscapeKey(() => {
    if (mobileOpen) closeMobile();
  }, mobileOpen);

  const toggleSection = useCallback(
    (sectionId: string) => {
      setCollapsed((prev) => {
        const next = { ...prev, [sectionId]: !prev[sectionId] };
        writeCollapsed(next);
        const section = NAV_SECTIONS.find((s) => s.id === sectionId);
        if (section) {
          announce(
            `${section.label} section ${next[sectionId] ? 'collapsed' : 'expanded'}`
          );
        }
        return next;
      });
    },
    [announce]
  );

  // On mobile, close the drawer when the user navigates so the content
  // area is visible after a tap.
  const handleNavClick = useCallback(() => {
    closeMobile();
  }, [closeMobile]);

  return (
    <>
      {/* Backdrop for mobile drawer.  Pointer-events disabled when closed
       * so it never blocks clicks on the underlying content. */}
      <div
        aria-hidden="true"
        onClick={closeMobile}
        className={
          'fixed inset-0 bg-black/50 z-40 md:hidden transition-opacity duration-200 ' +
          (mobileOpen
            ? 'opacity-100 pointer-events-auto'
            : 'opacity-0 pointer-events-none')
        }
      />

      <aside
        aria-label="Primary navigation"
        className={
          // Base styles: dark surface, scrollable list, full-height.
          'flex flex-col w-64 shrink-0 border-r bg-surface-secondary text-text-primary ' +
          'border-border ' +
          // Mobile: fixed overlay, slide in from left, hidden off-screen when closed.
          'fixed inset-y-0 left-0 z-50 transform transition-transform duration-200 ease-in-out ' +
          // Desktop: static, always visible, no transform.
          'md:static md:translate-x-0 md:z-auto ' +
          (mobileOpen ? 'translate-x-0' : '-translate-x-full')
        }
      >
        {/* Brand / close button row */}
        <div className="flex items-center justify-between px-4 h-14 border-b border-border shrink-0">
          <span className="text-lg font-semibold tracking-tight">
            <span className="text-accent">Open</span>AgentPlatform
          </span>
          {/* Mobile-only close button. */}
          <button
            type="button"
            onClick={closeMobile}
            className="md:hidden p-1.5 rounded-md text-text-muted hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent"
            aria-label="Close navigation menu"
          >
            <X className="w-5 h-5" aria-hidden="true" />
          </button>
        </div>

        {/* Scrollable nav area */}
        <nav
          aria-label="Sections"
          className="flex-1 overflow-y-auto py-3 px-2 space-y-4"
        >
          {NAV_SECTIONS.map((section) => {
            const isCollapsed = collapsed[section.id] ?? false;
            return (
              <SidebarSection
                key={section.id}
                section={section}
                collapsed={isCollapsed}
                onToggle={() => toggleSection(section.id)}
                criticalAlertCount={criticalAlertCount}
                pendingApprovalCount={pendingApprovalCount}
                onNavClick={handleNavClick}
              />
            );
          })}
        </nav>

        {/* Footer: user info + logout */}
        <div className="border-t border-border p-3 shrink-0">
          {user && (
            <div className="flex items-center gap-2 px-2 py-1.5">
              <div
                className="w-8 h-8 rounded-full bg-accent/20 text-accent flex items-center justify-center text-sm font-semibold"
                aria-hidden="true"
              >
                {user.name
                  ? user.name.charAt(0).toUpperCase()
                  : user.email.charAt(0).toUpperCase()}
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium truncate">
                  {user.name ?? user.email}
                </div>
                {user.name && (
                  <div className="text-xs text-text-muted truncate">{user.email}</div>
                )}
              </div>
            </div>
          )}
          <button
            type="button"
            onClick={() => logout()}
            className="w-full mt-2 flex items-center gap-2 px-2 py-2 rounded-md text-sm text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent"
          >
            <LogOut className="w-4 h-4" aria-hidden="true" />
            Sign out
          </button>
        </div>
      </aside>

      <AriaLiveRegion message={message} />
    </>
  );
}

// ---------------------------------------------------------------------------
// SidebarSection — collapsible group with header button + children list
// ---------------------------------------------------------------------------

interface SidebarSectionProps {
  section: NavSection;
  collapsed: boolean;
  onToggle: () => void;
  criticalAlertCount: number;
  pendingApprovalCount: number;
  onNavClick: () => void;
}

function SidebarSection({
  section,
  collapsed,
  onToggle,
  criticalAlertCount,
  pendingApprovalCount,
  onNavClick,
}: SidebarSectionProps) {
  const contentId = `sidebar-section-${section.id}`;
  const headerId = `sidebar-header-${section.id}`;
  const { getItemProps } = useRovingTabIndex(section.items.length);

  return (
    <div>
      <button
        type="button"
        id={headerId}
        onClick={onToggle}
        aria-expanded={!collapsed}
        aria-controls={contentId}
        className="w-full flex items-center justify-between px-2 py-1.5 text-xs font-semibold uppercase tracking-wider text-text-muted hover:text-text-primary focus:outline-none focus:ring-2 focus:ring-accent rounded"
      >
        <span>{section.label}</span>
        <ChevronDown
          className={
            'w-4 h-4 transition-transform duration-150 ' +
            (collapsed ? '-rotate-90' : 'rotate-0')
          }
          aria-hidden="true"
        />
      </button>

      {!collapsed && (
        <ul id={contentId} role="list" className="mt-1 space-y-0.5">
          {section.items.map((item, index) => {
            const badge =
              item.showAlertBadge && criticalAlertCount > 0
                ? criticalAlertCount
                : item.showPatchBadge && pendingApprovalCount > 0
                ? pendingApprovalCount
                : null;

            return (
              <li key={item.to}>
                <Link
                  to={item.to}
                  onClick={onNavClick}
                  activeProps={{
                    className:
                      'flex items-center gap-2 px-2 py-2 rounded-md text-sm font-medium bg-accent/15 text-accent border-l-2 border-accent',
                  }}
                  inactiveProps={{
                    className:
                      'flex items-center gap-2 px-2 py-2 rounded-md text-sm text-text-secondary hover:text-text-primary hover:bg-surface-tertiary border-l-2 border-transparent',
                  }}
                  {...getItemProps(index)}
                >
                  <item.icon className="w-4 h-4 shrink-0" aria-hidden="true" />
                  <span className="flex-1 truncate">{item.label}</span>
                  {badge !== null && (
                    <span
                      className="ml-auto inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1.5 text-xs font-bold rounded-full bg-red-500/20 text-red-400"
                      aria-label={`${badge} pending`}
                    >
                      {badge}
                    </span>
                  )}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}