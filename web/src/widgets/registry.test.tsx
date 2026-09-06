import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { initialClockState } from '../core/clock';
import type { Widget } from '../types/api';
import { WidgetSlot, findWidget, isRenderedWidgetType } from './registry';

const context = { clock: initialClockState, nowMs: Date.parse('2026-09-05T02:07:09Z') };

const clockWidget: Widget = {
  type: 'clock',
  payload: {
    timezone: 'Asia/Singapore',
    server_time: '2026-09-05T02:07:09Z',
    utc_offset_seconds: 28800,
  },
};

describe('widget registry (design §6.7)', () => {
  it('renders a known widget', () => {
    render(<WidgetSlot widget={clockWidget} context={context} />);
    expect(screen.getByText('10:07')).toBeDefined();
  });

  it('hides an unknown widget type entirely', () => {
    const unknown: Widget = { type: 'experimental_ticker', payload: { text: 'nope' } };
    expect(isRenderedWidgetType(unknown.type)).toBe(false);
    const { container } = render(<WidgetSlot widget={unknown} context={context} />);
    expect(container.innerHTML).toBe('');
  });

  it('finds a widget payload by type', () => {
    expect(findWidget([clockWidget], 'clock')).not.toBeNull();
    expect(findWidget([clockWidget], 'nas')).toBeNull();
  });

  it('shows the time-unverified badge when no server time has arrived', () => {
    render(<WidgetSlot widget={clockWidget} context={context} />);
    expect(screen.getByText(/time unverified/)).toBeDefined();
  });
});
