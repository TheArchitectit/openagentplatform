// ThemeProvider — manages light / dark / system colour mode for the app.
//
// On mount the stored preference is read from localStorage (key
// "oap-theme").  When the stored value is "system" (or no value is set)
// the OS-level prefers-color-scheme media query drives the resolved
// theme.  On every change the preference is persisted and the "dark"
// class is toggled on <html> so the CSS variable system in
// styles/themes.css applies the correct palette.
//
// Components consume the context through the useTheme() hook.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';

export type Theme = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

interface ThemeContextValue {
  /** The user-selected preference (may be "system"). */
  theme: Theme;
  /** The actual palette being applied ("light" or "dark"). */
  resolvedTheme: ResolvedTheme;
  /** Set the user's explicit preference. */
  setTheme: (theme: Theme) => void;
  /** Flip between light and dark, preserving a system preference. */
  toggleTheme: () => void;
}

const STORAGE_KEY = 'oap-theme';

const ThemeContext = createContext<ThemeContextValue | null>(null);

/** Read the resolved (concrete) theme from an OS preference. */
function getSystemTheme(): ResolvedTheme {
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia('(prefers-color-scheme: light)').matches
    ? 'light'
    : 'dark';
}

/** Apply the concrete theme to <html> by toggling the dark class. */
function applyTheme(resolved: ResolvedTheme) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (resolved === 'dark') {
    root.classList.add('dark');
    root.classList.remove('light');
  } else {
    root.classList.add('light');
    root.classList.remove('dark');
  }
}

/** Resolve a stored preference to a concrete theme. */
function resolve(stored: Theme | null): ResolvedTheme {
  if (stored === 'light' || stored === 'dark') return stored;
  return getSystemTheme();
}

function readStored(): Theme | null {
  if (typeof window === 'undefined') return null;
  const v = window.localStorage.getItem(STORAGE_KEY);
  if (v === 'light' || v === 'dark' || v === 'system') return v;
  return null;
}

interface ThemeProviderProps {
  children: ReactNode;
  /** Override the default localStorage key. */
  storageKey?: string;
}

export function ThemeProvider({ children, storageKey = STORAGE_KEY }: ThemeProviderProps) {
  const [theme, setThemeState] = useState<Theme>(() => readStored() ?? 'system');
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(() => getSystemTheme());

  const resolvedTheme: ResolvedTheme = useMemo(() => {
    if (theme === 'light' || theme === 'dark') return theme;
    return systemTheme;
  }, [theme, systemTheme]);

  // Apply the class whenever the resolved theme changes.
  useEffect(() => {
    applyTheme(resolvedTheme);
  }, [resolvedTheme]);

  // Listen for OS preference changes when in "system" mode.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia('(prefers-color-scheme: light)');
    const handler = (e: MediaQueryListEvent) => {
      setSystemTheme(e.matches ? 'light' : 'dark');
    };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  // Persist preference.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(storageKey, theme);
  }, [theme, storageKey]);

  const setTheme = useCallback((next: Theme) => {
    setThemeState(next);
  }, []);

  const toggleTheme = useCallback(() => {
    setThemeState((prev) => {
      // When toggling from "system", switch to the opposite of the
      // currently resolved theme so the user sees an immediate change.
      const current: ResolvedTheme =
        prev === 'light' || prev === 'dark' ? prev : resolve(prev);
      return current === 'dark' ? 'light' : 'dark';
    });
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ theme, resolvedTheme, setTheme, toggleTheme }),
    [theme, resolvedTheme, setTheme, toggleTheme]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

/** Access the current theme context.  Throws if used outside a provider. */
export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return ctx;
}