import { useState, useRef, useEffect } from 'react';
import {
  Search,
  Bell,
  ChevronDown,
  LogOut,
  User as UserIcon,
  KeyRound,
  Sun,
  Moon,
  Menu,
  Settings,
} from 'lucide-react';
import { useNavigate } from '@tanstack/react-router';
import { getStoredUser, logout } from '@/lib/auth';
import { useTheme } from '@/lib/theme';
import { useEscapeKey, visuallyHidden } from '@/lib/a11y';
import { useSidebar } from '@/lib/sidebar';

export function Header() {
  const navigate = useNavigate();
  const user = getStoredUser();
  const { resolvedTheme, toggleTheme } = useTheme();
  const { toggleMobile } = useSidebar();
  const [menuOpen, setMenuOpen] = useState(false);
  const [notifOpen, setNotifOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const notifRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
      if (notifRef.current && !notifRef.current.contains(e.target as Node)) {
        setNotifOpen(false);
      }
    }
    if (menuOpen || notifOpen) {
      document.addEventListener('mousedown', onClick);
      return () => document.removeEventListener('mousedown', onClick);
    }
  }, [menuOpen, notifOpen]);

  // Close dropdowns on Escape.
  useEscapeKey(() => {
    setMenuOpen(false);
    setNotifOpen(false);
  }, menuOpen || notifOpen);

  function handleUserMenuKey(e: React.KeyboardEvent<HTMLButtonElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      setMenuOpen((v) => !v);
    }
    if (e.key === 'ArrowDown' && !menuOpen) {
      e.preventDefault();
      setMenuOpen(true);
      // Focus first menu item after render.
      setTimeout(() => {
        const first = menuRef.current?.querySelector<HTMLButtonElement>(
          '[role="menuitem"]'
        );
        first?.focus();
      }, 0);
    }
  }

  // Placeholder unread notification count — replaced with real data once
  // the alert inbox API is wired into a dedicated hook.
  const unreadCount = 0;

  return (
    <header className="sticky top-0 z-30 flex items-center justify-between h-16 px-6 border-b border-slate-800 bg-slate-950/80 backdrop-blur-xl">
      <div className="flex items-center gap-3 min-w-0">
        {/* Mobile hamburger */}
        <button
          type="button"
          onClick={toggleMobile}
          className="md:hidden p-2 -ml-1 rounded-lg text-gray-400 hover:text-white hover:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
          aria-label="Open navigation menu"
        >
          <Menu className="w-5 h-5" aria-hidden="true" />
        </button>

        {/* Page title / breadcrumb */}
        <h1 className="text-sm font-medium text-gray-300 truncate hidden sm:block">
          Dashboard
        </h1>
      </div>

      <div className="flex items-center gap-2">
        {/* Search */}
        <div className="hidden sm:flex relative">
          <Search
            className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 pointer-events-none"
            aria-hidden="true"
          />
          <input
            type="search"
            placeholder="Search…"
            aria-label="Search"
            className="h-9 w-64 pl-9 pr-3 text-sm rounded-lg bg-slate-800 border border-slate-700 text-white placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
        </div>

        {/* Theme toggle */}
        <button
          type="button"
          onClick={toggleTheme}
          className="p-2 rounded-lg text-gray-400 hover:text-white hover:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
          aria-label={`Switch to ${resolvedTheme === 'dark' ? 'light' : 'dark'} mode`}
        >
          {resolvedTheme === 'dark' ? (
            <Sun className="w-5 h-5" aria-hidden="true" />
          ) : (
            <Moon className="w-5 h-5" aria-hidden="true" />
          )}
        </button>

        {/* Notification bell */}
        <div ref={notifRef} className="relative">
          <button
            type="button"
            onClick={() => setNotifOpen((v) => !v)}
            className="p-2 rounded-lg text-gray-400 hover:text-white hover:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500 relative"
            aria-label={
              unreadCount > 0
                ? `Notifications, ${unreadCount} unread`
                : 'Notifications'
            }
            aria-haspopup="true"
            aria-expanded={notifOpen}
          >
            <Bell className="w-5 h-5" aria-hidden="true" />
            {unreadCount > 0 && (
              <span
                className="absolute top-1 right-1 inline-flex items-center justify-center min-w-[1rem] h-4 px-1 text-[0.625rem] font-bold rounded-full bg-red-500 text-white"
                aria-hidden="true"
              >
                {unreadCount > 99 ? '99+' : unreadCount}
              </span>
            )}
          </button>

          {notifOpen && (
            <div
              role="dialog"
              aria-label="Notifications"
              className="absolute right-0 mt-2 w-80 max-w-[calc(100vw-1rem)] rounded-lg border border-slate-700 bg-slate-800 shadow-2xl overflow-hidden"
            >
              <div className="px-4 py-3 border-b border-slate-700">
                <h3 className="text-sm font-semibold text-white">Notifications</h3>
              </div>
              <div className="p-4 text-sm text-gray-500 text-center">
                No new notifications.
              </div>
            </div>
          )}
        </div>

        {/* User menu */}
        <div ref={menuRef} className="relative">
          <button
            ref={menuButtonRef}
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            onKeyDown={handleUserMenuKey}
            className="flex items-center gap-1.5 p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-label="User menu"
          >
            <span
              className="w-7 h-7 rounded-full bg-blue-600 text-white flex items-center justify-center text-xs font-semibold"
              aria-hidden="true"
            >
              {user?.name
                ? user.name.charAt(0).toUpperCase()
                : user?.email?.charAt(0).toUpperCase() ?? '?'}
            </span>
            <ChevronDown
              className={
                'w-3.5 h-3.5 hidden sm:block transition-transform text-gray-400 ' +
                (menuOpen ? 'rotate-180' : '')
              }
              aria-hidden="true"
            />
          </button>

          {menuOpen && (
            <div
              role="menu"
              aria-label="User menu"
              className="absolute right-0 mt-2 w-56 rounded-lg border border-slate-700 bg-slate-800 shadow-2xl py-1"
            >
              {user && (
                <div className="px-3 py-2 border-b border-slate-700">
                  <div className="text-sm font-medium text-white truncate">
                    {user.name ?? user.email}
                  </div>
                  <div className="text-xs text-gray-400 truncate">
                    {user.email}
                  </div>
                </div>
              )}

              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  void navigate({ to: '/settings' });
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-300 hover:bg-slate-700 hover:text-white focus:bg-slate-700 focus:outline-none"
              >
                <UserIcon className="w-4 h-4" aria-hidden="true" />
                Profile
              </button>

              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  void navigate({ to: '/settings' });
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-300 hover:bg-slate-700 hover:text-white focus:bg-slate-700 focus:outline-none"
              >
                <Settings className="w-4 h-4" aria-hidden="true" />
                Settings
              </button>

              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  void navigate({ to: '/settings/api-keys' });
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-300 hover:bg-slate-700 hover:text-white focus:bg-slate-700 focus:outline-none"
              >
                <KeyRound className="w-4 h-4" aria-hidden="true" />
                API Keys
              </button>

              <div className="my-1 border-t border-slate-700" />

              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  logout();
                }}
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-red-400 hover:bg-red-500/10 focus:bg-red-500/10 focus:outline-none"
              >
                <LogOut className="w-4 h-4" aria-hidden="true" />
                Logout
              </button>
            </div>
          )}
        </div>
      </div>

      <span className={visuallyHidden}>End of header</span>
    </header>
  );
}
