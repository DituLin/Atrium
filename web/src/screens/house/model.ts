import type { HomeResponse, WeatherWidgetPayload } from '../../types/api';

/** Display model only. It is deliberately separate from the public House API:
 * that API currently has no rooms, device observations, history or controls. */
export type HouseDeviceKind = 'light' | 'ac' | 'aquarium' | 'purifier' | 'humidifier' | 'curtain' | 'robot' | 'lock' | 'heater' | 'other';
export type HouseObservationState = 'available' | 'stale' | 'unavailable';
export interface HouseReading { value: number | null; state: HouseObservationState; observedAt?: string | null }
export interface HouseRoom {
  id: string; name: string;
  /** Coordinates on the 1120 × 640 floor-plan canvas. */
  rect: { x: number; y: number; width: number; height: number };
  temperature: HouseReading; humidity: HouseReading;
}
export interface HouseDevice {
  id: string; name: string; roomId: string | null; kind: HouseDeviceKind;
  state: 'on' | 'off' | 'unknown' | 'unavailable'; description: string;
  observation: HouseObservationState;
  /** Optional, configured position on the floor-plan canvas; never inferred from a name. */
  position?: { x: number; y: number };
  brightnessPercent?: number;
}
export interface HouseSample { at: string; value: number | null }
export interface HouseSeries {
  samples: HouseSample[]; current: HouseReading;
  /** End of this real observation window. It must not advance when the UI rerenders. */
  endAt: string; maxGapMs?: number;
  reference?: { label: string; samples: HouseSample[] };
}
export interface HouseAquarium {
  name: string; roomId: string | null; waterTemperature: HouseReading; history: HouseSeries | null;
  light: string | null; filtration: string | null; nextFeeding: string | null;
  filterRemainingPercent: number | null;
}
export interface HouseWeather { temperature: number | null; text: string | null; updatedAt: string | null; stale: boolean }
export interface HouseDashboardModel {
  /** Empty means no configured floor plan; geometric slots are only placeholders. */
  rooms: HouseRoom[]; devices: HouseDevice[];
  temperature: HouseSeries | null; humidity: HouseSeries | null;
  aquarium: HouseAquarium | null; weather: HouseWeather | null;
}

export function houseDashboardFromHome(home: HomeResponse | null): HouseDashboardModel {
  const widget = home?.widgets.find(widget => widget.type === 'weather');
  const weather = widget?.payload as WeatherWidgetPayload | undefined;
  return {
    rooms: [], devices: [], temperature: null, humidity: null, aquarium: null,
    weather: weather ? {
      temperature: typeof weather.temperature_c === 'number' && Number.isFinite(weather.temperature_c) ? weather.temperature_c : null,
      text: weather.condition_text, updatedAt: weather.fetched_at, stale: weather.stale,
    } : null,
  };
}

export const FLOORPLAN_PLACEHOLDERS = [
  { x: 0, y: 0, width: 340, height: 230 }, { x: 340, y: 0, width: 220, height: 230 },
  { x: 560, y: 0, width: 560, height: 320 }, { x: 0, y: 230, width: 560, height: 410 },
  { x: 560, y: 320, width: 300, height: 320 }, { x: 860, y: 320, width: 260, height: 320 },
] as const;
