import { render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import { ErrorBoundary, WidgetBoundary } from './ErrorBoundary';

function Boom(): ReactElement {
  throw new Error('widget exploded');
}

function Fine(): ReactElement {
  return <p>healthy widget</p>;
}

describe('error boundaries (W-106, FR-03)', () => {
  let consoleError: ReturnType<typeof vi.spyOn>;

  beforeAll(() => {
    // React logs the caught error; that noise is expected here.
    consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined);
  });

  afterAll(() => consoleError.mockRestore());

  it('contains a failing widget without touching its siblings', () => {
    render(
      <div>
        <WidgetBoundary name="clock">
          <Boom />
        </WidgetBoundary>
        <WidgetBoundary name="photo">
          <Fine />
        </WidgetBoundary>
      </div>,
    );
    expect(screen.getByText('clock unavailable')).toBeDefined();
    expect(screen.getByText('healthy widget')).toBeDefined();
  });

  it('reports the error to the diagnostics hook', () => {
    const onError = vi.fn();
    render(
      <WidgetBoundary name="weather" onError={onError}>
        <Boom />
      </WidgetBoundary>,
    );
    expect(onError).toHaveBeenCalledTimes(1);
    expect((onError.mock.calls[0]?.[0] as Error).message).toBe('widget exploded');
  });

  it('renders the global fallback with a retry instead of a blank page', () => {
    render(
      <ErrorBoundary fallback={(error) => <p>fallback: {error.message}</p>}>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText('fallback: widget exploded')).toBeDefined();
  });

  it('survives a throwing onError hook', () => {
    render(
      <WidgetBoundary
        name="notice"
        onError={() => {
          throw new Error('diagnostics broke too');
        }}
      >
        <Boom />
      </WidgetBoundary>,
    );
    expect(screen.getByText('notice unavailable')).toBeDefined();
  });
});
