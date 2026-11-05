import { forwardRef, useCallback, useLayoutEffect, useRef, useState } from 'react';
import { onReadingKey } from './readingRegion';
import '../../styles/weather-attribution.css';

interface Props {
  provider?: string;
  attributions?: string[];
  className?: string;
  onTop?: () => void;
  onLeft?: () => void;
  onBottom?: () => void;
  /** Include overflowing statements in the House spatial remote navigation. */
  houseFocus?: boolean;
  /** Settings owns one reading region for complete source and license text. */
  readingMode?: 'self' | 'parent';
}

const OFFICIAL_URL = 'https://www.qweather.com';
function attributionLink(value: string): string | null {
  if (!/^https:\/\//i.test(value)) return null;
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}

/** Source links are separate from refresh controls. Long legal statements are
 * readable with the remote without adding every external link to its sequence. */
export const WeatherAttribution = forwardRef<HTMLDivElement, Props>(function WeatherAttribution({ provider, attributions, className = '', onTop, onLeft, onBottom, houseFocus, readingMode = 'self' }, ref) {
  const element = useRef<HTMLDivElement | null>(null);
  const focused = useRef(false), returnFocus = useRef(onTop);
  returnFocus.current = onTop;
  const [overflow, setOverflow] = useState(false);
  const visible = provider === 'qweather';
  const assignRef = useCallback((node: HTMLDivElement | null) => {
    element.current = node;
    if (typeof ref === 'function') ref(node);
    else if (ref) ref.current = node;
  }, [ref]);
  useLayoutEffect(() => {
    if (!visible && focused.current) { focused.current = false; returnFocus.current?.(); }
    // Font ink and integer scroll metrics can extend a single line by 2 px.
    const measure = () => setOverflow(!!element.current && element.current.scrollHeight > element.current.clientHeight + 2);
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [visible, attributions]);
  if (!visible) return null;
  const seen = new Set([`${OFFICIAL_URL}/`]);
  const statements: Array<{ text: string; href: string | null }> = [];
  for (const item of attributions ?? []) {
    if (typeof item !== 'string' || !item.trim()) continue;
    const text = item.trim(), href = attributionLink(text), key = href ?? text;
    if (seen.has(key)) continue;
    seen.add(key); statements.push({ text, href });
  }
  return <div className={`weather-attribution ${className}`} ref={assignRef} role="region" aria-label="天气数据来源" tabIndex={readingMode === 'self' ? -1 : undefined}
    data-house-focus={houseFocus && overflow ? 'weather-attribution' : undefined}
    onFocusCapture={() => { focused.current = true; }} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) focused.current = false; }}
    onKeyDown={readingMode === 'self' ? event => onReadingKey(event, { top: onTop, left: onLeft, bottom: onBottom }) : undefined}>
    <span className="weather-attribution__content"><span>天气数据：</span><a href={OFFICIAL_URL} target="_blank" rel="noopener noreferrer" tabIndex={-1} onMouseDown={event => event.preventDefault()}>和风天气 · QWeather</a>
      {statements.map(statement => <span className="weather-attribution__statement" key={statement.href ?? statement.text}><span aria-hidden="true"> · </span>{statement.href
        ? <a href={statement.href} target="_blank" rel="noopener noreferrer" tabIndex={-1} onMouseDown={event => event.preventDefault()}>{statement.text}</a> : statement.text}</span>)}
    </span>
  </div>;
});
