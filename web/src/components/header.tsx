import { useState, useRef, useEffect } from 'react';
import { Search, Bell, ChevronDown, LogOut, User as UserIcon, Globe } from 'lucide-react';
import { useNavigate } from '@tanstack/react-router';
import { getStoredUser, logout } from '@/lib/auth';

export function Header() {
  const navigate = useNavigate();
  const user = getStoredUser();
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    }
    if (menuOpen) {
      document.addEventListener('mousedown', onClick);
      return () => document.removeEventListener('mousedown', onClick);
    }
  }, [menuOpen]);

  const initials = user?.name
    ? user.name.split(' ').map((p) => p.charAt(0).toUpperCase()).slice(0, 2).join('')
    : user?.email?.charAt(0).toUpperCase() ?? 'U';

  return (
    <header className="h-14 shrink-0 border-b border-slate-800 bg-slate-900/50 backdrop-blur px-6 flex items-center justify-between gap-4">
      {/* Left: breadcrumbs / site */}
      <div className="flex items-center gap-2 text-sm text-slate-400">
        <Globe size={14} className="text-slate-500" />
        <span>Endpoints</span>
        <span className="text-slate-600">/</span>
        <span className="text-slate-200">Default site</span>
      </div>

      {/* Center: search */}
      <div className="flex-1 max-w-md">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <input
            type="search"
            placeholder="Search agents, checks, alerts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
          />
        </div>
      </div>

      {/* Right: actions */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          title="Notifications"
          className="relative h-9 w-9 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-100 hover:bg-slate-800 transition-colors"
        >
          <Bell size={16} />
          <span className="absolute top-2 right-2 h-1.5 w-1.5 rounded-full bg-rose-500" />
        </button>

        {/* User menu */}
        <div className="relative" ref={menuRef}>
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            className="flex items-center gap-2 h-9 pl-1.5 pr-2 rounded-md hover:bg-slate-800 transition-colors"
          >
            <div className="h-7 w-7 rounded-full bg-indigo-600 flex items-center justify-center text-xs font-medium text-white">
              {initials}
            </div>
            <span className="hidden sm:block text-sm text-slate-200 max-w-[120px] truncate">
              {user?.name ?? user?.email ?? 'Account'}
            </span>
            <ChevronDown size={14} className="text-slate-400" />
          </button>

          {menuOpen && (
            <div className="absolute right-0 top-full mt-1 w-56 rounded-md border border-slate-800 bg-slate-900 shadow-xl py-1 z-50">
              <div className="px-3 py-2 border-b border-slate-800">
                <p className="text-sm text-slate-100 truncate">{user?.name ?? 'User'}</p>
                <p className="text-xs text-slate-500 truncate">{user?.email ?? ''}</p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setMenuOpen(false);
                  navigate({ to: '/settings' });
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
              >
                <UserIcon size={14} />
                <span>Profile</span>
              </button>
              <button
                type="button"
                onClick={() => {
                  setMenuOpen(false);
                  logout();
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-rose-400 hover:bg-slate-800 transition-colors"
              >
                <LogOut size={14} />
                <span>Logout</span>
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
