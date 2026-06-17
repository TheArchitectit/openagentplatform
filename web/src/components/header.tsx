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
    <header className="sticky top-0 z-30 flex items-center justify-between gap-2 h-14 px-3 md:px-4 border-b border-border bg-surface-secondary/95 backdrop-blur supports-[backdrop-filter]:bg-surface-secondary/80">
      <div className="flex items-center gap-2 min-w-0">
        {/* Mobile hamburger */}
        <button
          type="button"
          onClick={toggleMobile}
          className="md:hidden p-2 -ml-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent"
          aria-label="Open navigation menu"
        >
          <Menu className="w-5 h-5" aria-hidden="true" />
        </button>

        {/* Search (hidden on small screens to save space) */}
        <div className="hidden sm:flex relative">
          <Search
            className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted pointer-events-none"
            aria-hidden="true"
          />
          <input
            type="search"
            placeholder="Search…"
            aria-label="Search"
            className="w-48 md:w-64 pl-8 pr-3 py-1.5 text-sm rounded-md bg-surface-tertiary border border-border text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent focus:border-accent"
          />
        </div>
      </div>

      <div className="flex items-center gap-1 sm:gap-2">
        {/* Theme toggle */}
        <button
          type="button"
          onClick={toggleTheme}
          className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent"
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
            className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent relative"
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
              className="absolute right-0 mt-2 w-80 max-w-[calc(100vw-1rem)] rounded-md border border-border bg-surface-secondary shadow-lg overflow-hidden"
            >
              <div className="px-4 py-3 border-b border-border">
                <h3 className="text-sm font-semibold">Notifications</h3>
              </div>
              <div className="p-4 text-sm text-text-muted text-center">
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
            className="flex items-center gap-1.5 p-1.5 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary focus:outline-none focus:ring-2 focus:ring-accent"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-label="User menu"
          >
            <span
              className="w-7 h-7 rounded-full bg-accent/20 text-accent flex items-center justify-center text-xs font-semibold"
              aria-hidden="true"
            >
              {user?.name
                ? user.name.charAt(0).toUpperCase()
                : user?.email?.charAt(0).toUpperCase() ?? '?'}
            </span>
            <ChevronDown
              className={
                'w-3.5 h-3.5 hidden sm:block transition-transform ' +
                (menuOpen ? 'rotate-180' : '')
              }
              aria-hidden="true"
            />
          </button>

          {menuOpen && (
            <div
              role="menu"
              aria-label="User menu"
              className="absolute right-0 mt-2 w-56 rounded-md border border-border bg-surface-secondary shadow-lg py-1"
            >
              {user && (
                <div className="px-3 py-2 border-b border-border">
                  <div className="text-sm font-medium truncate">
                    {user.name ?? user.email}
                  </div>
                  <div className="text-xs text-text-muted truncate">
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
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:bg-surface-tertiary hover:text-text-primary focus:bg-surface-tertiary focus:outline-none"
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
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:bg-surface-tertiary hover:text-text-primary focus:bg-surface-tertiary focus:outline-none"
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
                className="w-full flex items-center gap-2 px-3 py-2 text-sm text-text-secondary hover:bg-surface-tertiary hover:text-text-primary focus:bg-surface-tertiary focus:outline-none"
              >
                <KeyRound className="w-4 h-4" aria-hidden="true" />
                API Keys
              </button>

              <div className="my-1 border-t border-border" />

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