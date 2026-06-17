// Accessibility utilities for the OpenAgentPlatform web app.
//
// Provides:
//   - SkipToContent: a skip-link component that lets keyboard / screen reader
//     users jump straight to the main content area.
//   - useFocusTrap: traps focus inside a container (modal, dialog).
//   - useAriaAnnounce: a live-region hook for screen reader announcements.
//   - visuallyHidden: CSS class for screen-reader-only text.

import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type RefObject,
} from 'react';

// ---------------------------------------------------------------------------
// visuallyHidden — CSS class for screen-reader-only content
// ---------------------------------------------------------------------------

/**
 * Class name that visually hides an element while keeping it accessible to
 * assistive technology.  Use for extra context that screen reader users need
 * but sighted users do not.
 */
export const visuallyHidden =
  'sr-only absolute w-px h-px p-0 -m-px overflow-hidden whitespace-nowrap border-0 ' +
  'clip-[rect(0,0,0,0)] [clip-path:inset(50%)]';

// ---------------------------------------------------------------------------
// Focusable selector
// ---------------------------------------------------------------------------

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
  'audio[controls]',
  'video[controls]',
  '[contenteditable]:not([contenteditable="false"])',
].join(',');

/** Return all focusable descendants of `container`, in tab order. */
export function getFocusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
  ).filter(
    (el) => !el.hasAttribute('inert') && el.offsetParent !== null
  );
}

// ---------------------------------------------------------------------------
// SkipToContent — skip link component
// ---------------------------------------------------------------------------

export interface SkipToContentProps {
  /** ID of the element to jump to.  Defaults to "main-content". */
  targetId?: string;
  /** Visible label.  Defaults to "Skip to main content". */
  label?: string;
}

/**
 * Renders a skip-link that is visually hidden until it receives keyboard
 * focus, then jumps to the target element.  Place as the first child of
 * the root layout.
 */
export function SkipToContent({
  targetId = 'main-content',
  label = 'Skip to main content',
}: SkipToContentProps) {
  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLAnchorElement>) => {
      e.preventDefault();
      const target = document.getElementById(targetId);
      if (target) {
        target.setAttribute('tabindex', '-1');
        target.focus();
        target.scrollIntoView({ behavior: 'smooth' });
      }
    },
    [targetId]
  );

  return (
    <a
      href={`#${targetId}`}
      onClick={handleClick}
      className={
        'sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-[9999] ' +
        'focus:px-4 focus:py-2 focus:bg-accent focus:text-white focus:rounded-md ' +
        'focus:outline-none focus:ring-2 focus:ring-accent focus:ring-offset-2 ' +
        'focus:ring-offset-surface-primary text-sm font-medium transition-none'
      }
    >
      {label}
    </a>
  );
}

// ---------------------------------------------------------------------------
// useFocusTrap — trap focus within a container
// ---------------------------------------------------------------------------

export interface UseFocusTrapOptions {
  /** Whether the trap is active.  Defaults to `true`. */
  active?: boolean;
  /** Restore focus to the previously focused element on unmount. */
  restoreFocus?: boolean;
  /** Auto-focus the first focusable element on mount. */
  autoFocus?: boolean;
}

/**
 * Hook that traps keyboard focus inside `containerRef` while `active` is
 * true.  Handles Tab / Shift+Tab cycling and Escape to call `onEscape`.
 */
export function useFocusTrap<T extends HTMLElement>(
  containerRef: RefObject<T | null>,
  options: UseFocusTrapOptions = {}
) {
  const { active = true, restoreFocus = true, autoFocus = true } = options;
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!active) return;
    const container = containerRef.current;
    if (!container) return;

    // Remember the element that had focus before the trap activated.
    previouslyFocusedRef.current = document.activeElement as HTMLElement | null;

    // Auto-focus first focusable child.
    if (autoFocus) {
      const focusable = getFocusableElements(container);
      if (focusable.length > 0) {
        focusable[0].focus();
      } else {
        container.setAttribute('tabindex', '-1');
        container.focus();
      }
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Tab') return;
      const focusable = getFocusableElements(container!);
      if (focusable.length === 0) {
        e.preventDefault();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const activeEl = document.activeElement as HTMLElement | null;

      if (e.shiftKey) {
        if (activeEl === first || !container!.contains(activeEl)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (activeEl === last || !container!.contains(activeEl)) {
          e.preventDefault();
          first.focus();
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      // Restore focus to the element that opened the trap.
      if (restoreFocus && previouslyFocusedRef.current) {
        previouslyFocusedRef.current.focus();
      }
    };
  }, [active, autoFocus, restoreFocus, containerRef]);
}

// ---------------------------------------------------------------------------
// useAriaAnnounce — live region for screen reader announcements
// ---------------------------------------------------------------------------

/**
 * Hook that provides a screen-reader announcement function.  Messages
 * are inserted into a visually hidden live region with `aria-live="polite"`.
 */
export function useAriaAnnounce() {
  const [message, setMessage] = useState('');
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const announce = useCallback((msg: string) => {
    // Clear any pending reset so rapid announcements don't cancel each other.
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    setMessage('');
    // Use a microtask delay so identical successive messages re-announce.
    requestAnimationFrame(() => {
      setMessage(msg);
      timeoutRef.current = setTimeout(() => setMessage(''), 1000);
    });
  }, []);

  // Cleanup pending timeout on unmount.
  useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  return { message, announce };
}

/**
 * Renders the live-region element.  Place once at the app root and pass
 * the `message` from `useAriaAnnounce`.  Each consumer can use its own
 * region by rendering this directly.
 */
export function AriaLiveRegion({ message }: { message: string }) {
  return (
    <div
      role="status"
      aria-live="polite"
      aria-atomic="true"
      className={visuallyHidden}
    >
      {message}
    </div>
  );
}

// ---------------------------------------------------------------------------
// useRovingTabIndex — arrow-key navigation for lists / menus
// ---------------------------------------------------------------------------

/**
 * Hook that implements the "roving tabindex" pattern for keyboard
 * navigation in a list of items.  Returns:
 *   - `getItemProps(index)` — spreads tabIndex, onKeyDown, ref
 *   - `activeIndex` — the currently focused item
 *
 * Use for sidebar nav, tab lists, or any widget where only one item
 * should be in the tab order at a time.
 */
export function useRovingTabIndex(itemCount: number) {
  const [activeIndex, setActiveIndex] = useState(0);
  const itemRefs = useRef<(HTMLElement | null)[]>([]);

  const setItemRef = useCallback(
    (index: number) => (el: HTMLElement | null) => {
      itemRefs.current[index] = el;
    },
    []
  );

  const getItemProps = useCallback(
    (index: number) => ({
      tabIndex: index === activeIndex ? 0 : -1,
      ref: setItemRef(index),
      onKeyDown: (e: React.KeyboardEvent) => {
        let next = activeIndex;
        switch (e.key) {
          case 'ArrowDown':
          case 'ArrowRight':
            e.preventDefault();
            next = (activeIndex + 1) % itemCount;
            break;
          case 'ArrowUp':
          case 'ArrowLeft':
            e.preventDefault();
            next = (activeIndex - 1 + itemCount) % itemCount;
            break;
          case 'Home':
            e.preventDefault();
            next = 0;
            break;
          case 'End':
            e.preventDefault();
            next = itemCount - 1;
            break;
          default:
            return;
        }
        setActiveIndex(next);
        itemRefs.current[next]?.focus();
      },
    }),
    [activeIndex, itemCount, setItemRef]
  );

  return { activeIndex, getItemProps };
}

// ---------------------------------------------------------------------------
// useEscapeKey — listen for Escape key
// ---------------------------------------------------------------------------

/**
 * Calls `handler` when the Escape key is pressed.  Pass `active = false`
 * to temporarily disable.
 */
export function useEscapeKey(handler: () => void, active = true) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    if (!active) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') handlerRef.current();
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [active]);
}

// ---------------------------------------------------------------------------
// generateId — unique ID for aria-labelledby / aria-describedby targets
// ---------------------------------------------------------------------------

/**
 * Returns a stable, unique ID suitable for ARIA attributes.
 * Uses React's `useId` under the hood.
 */
export function useAriaId(prefix = 'aria'): string {
  const id = useId();
  return `${prefix}-${id.replace(/:/g, '')}`;
}
