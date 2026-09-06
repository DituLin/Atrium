/**
 * Roving focus for remote control navigation.
 *
 * One element in a group is tabbable at a time; direction keys move the focus
 * index and the focused element receives `.is-focused` in addition to the real
 * DOM focus, so the ring survives engines with a weak `:focus-visible`.
 * The visible cue is an outline plus a scale change — never colour alone
 * (PRD FR-01/FR-04).
 */

import type { ReactElement, ReactNode } from 'react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';

import type { RemoteKey } from './keys';
import { isDirection, mapRemoteKey } from './keys';
import { nextFocusIndex } from './focusNav';

interface FocusGroupContextValue {
  activeIndex: number;
  register: (element: HTMLElement | null, index: number) => void;
  setActiveIndex: (index: number) => void;
}

const FocusGroupContext = createContext<FocusGroupContextValue | null>(null);

export interface FocusGroupProps {
  columns?: number;
  count: number;
  /** Controlled focus index; omit to let the group keep its own. */
  index?: number;
  /** Fired whenever the focused index changes, controlled or not. */
  onIndexChange?: (index: number) => void;
  /** Enter on the focused item. */
  onActivate?: (index: number) => void;
  /**
   * Delegates direction keys to the owner (a reducer keeps the index), which
   * returns false when the key did not move the focus. Without it the group
   * computes the move itself with `nextFocusIndex`.
   */
  onDirection?: (direction: RemoteKey) => boolean;
  /** Direction keys that did not move focus bubble out (e.g. Left on col 0). */
  onEscapeEdge?: (direction: RemoteKey) => void;
  children: ReactNode;
  className?: string;
}

export function FocusGroup(props: FocusGroupProps): ReactElement {
  const {
    columns = 1,
    count,
    onActivate,
    onDirection,
    onEscapeEdge,
    onIndexChange,
    children,
    className,
  } = props;
  const [internalIndex, setInternalIndex] = useState(0);
  const controlled = props.index !== undefined;
  const rawIndex = controlled ? (props.index ?? 0) : internalIndex;
  // Derived, not corrected in an effect: a shrinking list clamps on render.
  const activeIndex = count > 0 ? Math.min(rawIndex, count - 1) : 0;
  const elements = useRef<Map<number, HTMLElement>>(new Map());

  const setActiveIndex = useCallback(
    (index: number) => {
      if (!controlled) setInternalIndex(index);
      onIndexChange?.(index);
    },
    [controlled, onIndexChange],
  );

  const register = useCallback((element: HTMLElement | null, index: number) => {
    if (element) elements.current.set(index, element);
    else elements.current.delete(index);
  }, []);

  const move = useCallback(
    (direction: RemoteKey) => {
      if (onDirection) {
        if (!onDirection(direction)) onEscapeEdge?.(direction);
        return;
      }
      const next = nextFocusIndex(activeIndex, direction, { count, columns });
      if (next === activeIndex || next < 0) {
        onEscapeEdge?.(direction);
        return;
      }
      setActiveIndex(next);
      elements.current.get(next)?.focus();
    },
    [activeIndex, count, columns, onDirection, onEscapeEdge, setActiveIndex],
  );

  // A controlled group follows its owner's index with real DOM focus, so the
  // remote keeps working after a page load moves the selection.
  const focused = useRef(-1);
  useEffect(() => {
    if (!controlled || count === 0 || focused.current === activeIndex) return;
    focused.current = activeIndex;
    elements.current.get(activeIndex)?.focus();
  }, [controlled, activeIndex, count]);

  const handleRemoteKey = useCallback(
    (key: RemoteKey) => {
      if (isDirection(key)) {
        move(key);
        return true;
      }
      if (key === 'enter') {
        onActivate?.(activeIndex);
        return true;
      }
      return false;
    },
    [move, onActivate, activeIndex],
  );

  const value = useMemo<FocusGroupContextValue>(
    () => ({ activeIndex, register, setActiveIndex }),
    [activeIndex, register, setActiveIndex],
  );

  return (
    <FocusGroupContext.Provider value={value}>
      <div
        className={className}
        data-focus-group=""
        onKeyDown={(event) => {
          const mapped = mapRemoteKey({ key: event.key, keyCode: event.keyCode });
          if (mapped && handleRemoteKey(mapped)) event.preventDefault();
        }}
      >
        {children}
      </div>
    </FocusGroupContext.Provider>
  );
}

export interface FocusableProps {
  index: number;
  children: ReactNode;
  className?: string;
  onActivate?: () => void;
  label?: string;
}

/** One focusable cell inside a `FocusGroup`. */
export function Focusable(props: FocusableProps): ReactElement {
  const group = useContext(FocusGroupContext);
  const ref = useRef<HTMLDivElement | null>(null);
  const { index, children, className, onActivate, label } = props;
  const isActive = group ? group.activeIndex === index : index === 0;

  useEffect(() => {
    group?.register(ref.current, index);
    return () => group?.register(null, index);
  }, [group, index]);

  return (
    <div
      ref={ref}
      role="button"
      aria-label={label}
      tabIndex={isActive ? 0 : -1}
      className={[className, 'focusable', isActive ? 'is-focused' : ''].filter(Boolean).join(' ')}
      onFocus={() => group?.setActiveIndex(index)}
      onClick={onActivate}
      onKeyDown={(event) => {
        if (event.key === 'Enter') {
          event.preventDefault();
          onActivate?.();
        }
      }}
    >
      {children}
    </div>
  );
}
