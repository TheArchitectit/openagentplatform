// Roving-tabindex hook, split out of a11y.tsx to keep that file under the
// size gate. Re-exported from a11y.tsx so importers need no changes.

import { useCallback, useRef, useState } from 'react';

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
        let next: number;
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
