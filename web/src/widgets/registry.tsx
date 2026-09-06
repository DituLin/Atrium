/**
 * Widget registry (design §6.7, PRD FR-01). Widget types are a closed
 * whitelist: anything the server adds later renders as nothing at all rather
 * than as a broken card, and no payload is ever interpreted as markup or code.
 */

import type { ReactElement, ReactNode } from 'react';

import type { ClockState } from '../core/clock';
import type {
  ClockWidgetPayload,
  NoticeWidgetPayload,
  WeatherWidgetPayload,
  Widget,
} from '../types/api';
import { WidgetBoundary } from '../ui/ErrorBoundary';
import { ClockWidget } from './ClockWidget';
import { NoticeWidget } from './NoticeWidget';
import { WeatherWidget } from './WeatherWidget';

export interface WidgetRenderContext {
  clock: ClockState;
  nowMs: number;
}

/**
 * Types this registry renders as a card. `nas` is drawn by the status bar and
 * `photo` by the dashboard's own pane (it owns the slideshow), so neither is
 * listed here; unknown types stay hidden either way.
 */
export const RENDERED_WIDGET_TYPES = ['clock', 'weather', 'notice'] as const;
export type RenderedWidgetType = (typeof RENDERED_WIDGET_TYPES)[number];

export function isRenderedWidgetType(type: string): type is RenderedWidgetType {
  return (RENDERED_WIDGET_TYPES as readonly string[]).includes(type);
}

export function findWidget<T>(widgets: readonly Widget[], type: string): T | null {
  for (const widget of widgets) {
    if (widget.type === type) return widget.payload as T;
  }
  return null;
}

/** Renders one widget, or `null` when the type is unknown to this client. */
export function renderWidget(widget: Widget, context: WidgetRenderContext): ReactNode {
  switch (widget.type) {
    case 'clock':
      return (
        <ClockWidget
          payload={widget.payload as ClockWidgetPayload}
          clock={context.clock}
          nowMs={context.nowMs}
        />
      );
    case 'weather':
      return <WeatherWidget payload={widget.payload as WeatherWidgetPayload} />;
    case 'notice':
      return <NoticeWidget payload={widget.payload as NoticeWidgetPayload} />;
    default:
      return null;
  }
}

export interface WidgetSlotProps {
  widget: Widget;
  context: WidgetRenderContext;
}

/** A widget plus its own error boundary: one failure never spreads (W-106). */
export function WidgetSlot(props: WidgetSlotProps): ReactElement | null {
  if (!isRenderedWidgetType(props.widget.type)) return null;
  return (
    <WidgetBoundary name={props.widget.type}>
      {renderWidget(props.widget, props.context)}
    </WidgetBoundary>
  );
}
