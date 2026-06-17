// SidebarContext — shared mobile-sidebar open/close state.
//
// The hamburger button in the Header and the Sidebar overlay need to share
// "is the mobile drawer open?" state.  Lifting it into a tiny context
// avoids prop-drilling and keeps the two components decoupled.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';

interface SidebarContextValue {
  /** Whether the mobile drawer is currently open. */
  mobileOpen: boolean;
  /** Open the mobile drawer. */
  openMobile: () => void;
  /** Close the mobile drawer. */
  closeMobile: () => void;
  /** Toggle the mobile drawer. */
  toggleMobile: () => void;
}

const SidebarContext = createContext<SidebarContextValue | null>(null);

/**
 * Returns the current sidebar context.  Throws when used outside of a
 * <SidebarProvider> — fail loudly so we never accidentally rely on
 * "always open" desktop layout in a component that's supposed to live
 * inside the mobile overlay.
 */
export function useSidebar(): SidebarContextValue {
  const ctx = useContext(SidebarContext);
  if (!ctx) {
    throw new Error('useSidebar must be used within a SidebarProvider');
  }
  return ctx;
}

interface SidebarProviderProps {
  children: ReactNode;
}

export function SidebarProvider({ children }: SidebarProviderProps) {
  const [mobileOpen, setMobileOpen] = useState(false);

  const openMobile = useCallback(() => setMobileOpen(true), []);
  const closeMobile = useCallback(() => setMobileOpen(false), []);
  const toggleMobile = useCallback(() => setMobileOpen((v) => !v), []);

  // Close the mobile drawer on route change is handled by the consumer via
  // useEscapeKey + backdrop click.  We also auto-close when the viewport
  // grows past the mobile breakpoint so the user is not left with a
  // "stuck open" drawer after rotating a tablet.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia('(min-width: 768px)');
    const handler = (e: MediaQueryListEvent) => {
      if (e.matches) setMobileOpen(false);
    };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  const value = useMemo<SidebarContextValue>(
    () => ({ mobileOpen, openMobile, closeMobile, toggleMobile }),
    [mobileOpen, openMobile, closeMobile, toggleMobile]
  );

  return <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>;
}