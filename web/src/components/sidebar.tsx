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
} from 'lucide-react';
import { logout, getStoredUser } from '@/lib/auth';

interface NavItem {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
}

const navItems: NavItem[] = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/agents', label: 'Agents', icon: Bot },
  { to: '/checks', label: 'Checks', icon: Activity },
  { to: '/alerts', label: 'Alerts', icon: BellRing },
  { to: '/policies', label: 'Policies', icon: ShieldCheck },
  { to: '/patches', label: 'Patches', icon: Wrench },
  { to: '/scripts', label: 'Scripts', icon: FileCode2 },
  { to: '/settings', label: 'Settings', icon: Settings },
];

export function Sidebar() {
  const user = getStoredUser();
  const initials = user?.name
    ? user.name.split(' ').map((p) => p.charAt(0).toUpperCase()).slice(0, 2).join('')
    : user?.email?.charAt(0).toUpperCase() ?? 'U';

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
        {navItems.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.to}
              to={item.to}
              className="flex items-center gap-3 px-3 py-2 rounded-md text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
              activeProps={{
                className: 'flex items-center gap-3 px-3 py-2 rounded-md text-sm bg-slate-800 text-white',
              }}
            >
              <Icon size={16} />
              <span>{item.label}</span>
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
