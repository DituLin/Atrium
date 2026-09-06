/**
 * Error boundaries (W-106, PRD FR-03: a failing widget must never white-screen
 * the TV). Two levels:
 *   - `WidgetBoundary` replaces one widget with a small placeholder.
 *   - `AppBoundary` catches everything else and offers a retry.
 */

import type { ErrorInfo, ReactElement, ReactNode } from 'react';
import { Component } from 'react';

export interface ErrorBoundaryProps {
  children: ReactNode;
  /** Rendered instead of the children when they throw. */
  fallback: (error: Error, reset: () => void) => ReactNode;
  /** Diagnostics hook; must not throw. */
  onError?: (error: Error, info: ErrorInfo) => void;
}

interface ErrorBoundaryState {
  error: Error | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    try {
      this.props.onError?.(error, info);
    } catch {
      /* a failing diagnostics hook must not take the screen down */
    }
  }

  private readonly reset = (): void => {
    this.setState({ error: null });
  };

  override render(): ReactNode {
    if (this.state.error) return this.props.fallback(this.state.error, this.reset);
    return this.props.children;
  }
}

export interface WidgetBoundaryProps {
  /** Widget type, shown in the placeholder so the operator can diagnose it. */
  name: string;
  children: ReactNode;
  onError?: (error: Error, info: ErrorInfo) => void;
}

export function WidgetBoundary(props: WidgetBoundaryProps): ReactElement {
  return (
    <ErrorBoundary
      {...(props.onError ? { onError: props.onError } : {})}
      fallback={() => (
        <div className="widget widget--error" role="status">
          <span className="widget-error__icon" aria-hidden="true">
            !
          </span>
          <span className="widget-error__text">{props.name} unavailable</span>
        </div>
      )}
    >
      {props.children}
    </ErrorBoundary>
  );
}
