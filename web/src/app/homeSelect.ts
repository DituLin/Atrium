/**
 * Typed reads of the `/home` snapshot (design §6.7). The snapshot is a list of
 * widget envelopes, so every screen that needs the clock zone, the photo counts
 * or the NAS health would otherwise repeat the same search.
 */

import type { ClockFormatOptions } from '../core/clock';
import type {
  ClockWidgetPayload,
  HomeResponse,
  NasWidgetPayload,
  PhotoWidgetPayload,
} from '../types/api';

function payloadOf<T>(home: HomeResponse | null, type: string): T | null {
  if (!home) return null;
  for (const widget of home.widgets) {
    if (widget.type === type) return widget.payload as T;
  }
  return null;
}

export function clockWidget(home: HomeResponse | null): ClockWidgetPayload | null {
  return payloadOf<ClockWidgetPayload>(home, 'clock');
}

export function photoWidget(home: HomeResponse | null): PhotoWidgetPayload | null {
  return payloadOf<PhotoWidgetPayload>(home, 'photo');
}

export function nasWidget(home: HomeResponse | null): NasWidgetPayload | null {
  return payloadOf<NasWidgetPayload>(home, 'nas');
}

/**
 * Formatting options for every date the client prints. Falls back to the
 * browser's own zone with a zero offset when no snapshot has arrived yet, so a
 * caption never renders as `Invalid Date`.
 */
export function clockOptions(home: HomeResponse | null): ClockFormatOptions {
  const clock = clockWidget(home);
  if (clock) {
    return { timezone: clock.timezone, utcOffsetSeconds: clock.utc_offset_seconds };
  }
  return { timezone: home?.home.timezone ?? 'UTC', utcOffsetSeconds: 0 };
}
