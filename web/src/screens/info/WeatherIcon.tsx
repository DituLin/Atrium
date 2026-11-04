import { weatherConditionInfo } from './weatherPresentation';

const ICONS = {
  sun: 'M12 8a4 4 0 1 0 0 8a4 4 0 1 0 0-8 M12 2v2 M12 20v2 M4.9 4.9l1.4 1.4 M17.7 17.7l1.4 1.4 M2 12h2 M20 12h2 M4.9 19.1l1.4-1.4 M17.7 6.3l1.4-1.4',
  cloud: 'M7 18h10a4 4 0 0 0 0-8 6 6 0 0 0-11.5 1.5A3.3 3.3 0 0 0 7 18z',
  partly: 'M8 3v1.5 M3 8h1.5 M4.5 4.5l1 1 M11.5 4.5l-1 1 M5.3 10.6a3 3 0 0 1 5.4-3.3 M9 20h8a3.5 3.5 0 0 0 0-7 5 5 0 0 0-9.6 1.2A2.9 2.9 0 0 0 9 20z',
  rain: 'M7 15h10a4 4 0 0 0 0-8 6 6 0 0 0-11.5 1.5A3.3 3.3 0 0 0 7 15z M8 18l-1 2.5 M12 18l-1 2.5 M16 18l-1 2.5',
  snow: 'M7 14h10a4 4 0 0 0 0-8 6 6 0 0 0-11.5 1.5A3.3 3.3 0 0 0 7 14z M8 18h.01 M12 20h.01 M16 18h.01',
  storm: 'M7 15h10a4 4 0 0 0 0-8 6 6 0 0 0-11.5 1.5A3.3 3.3 0 0 0 7 15z M13 16l-3 4h4l-3 4',
  fog: 'M5 8h14 M3 12h18 M5 16h14 M7 20h10',
  unknown: 'M9.5 8a2.5 2.5 0 0 1 5 .4c0 2-2.5 2-2.5 4 M12 16h.01 M12 2a10 10 0 1 0 0 20a10 10 0 0 0 0-20',
};

/** Open-Meteo WMO codes. Unknown is an explicit question icon. */
export function WeatherIcon({ code, text, className = '' }: { code: string | number | null | undefined; text?: string | null; className?: string }) {
  const { kind } = weatherConditionInfo(code, text);
  return <svg className={`today-weather__icon today-weather__icon--${kind} ${className}`} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d={ICONS[kind]} /></svg>;
}
