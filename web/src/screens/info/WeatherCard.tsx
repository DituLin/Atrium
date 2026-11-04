import { forwardRef, useId, useMemo } from 'react';
import type { ComponentPropsWithoutRef } from 'react';
import type { ClockFormatOptions } from '../../core/clock';
import type { WeatherWidgetPayload } from '../../types/api';
import { RemoteButton } from '../../ui/RemoteButton';
import { WeatherIcon } from './WeatherIcon';
import { presentWeather, weatherConditionInfo } from './weatherPresentation';

type Props = Omit<ComponentPropsWithoutRef<typeof RemoteButton>, 'children'> & {
  weather: WeatherWidgetPayload | null;
  options: ClockFormatOptions | null;
  now: number;
};

export const WeatherCard = forwardRef<HTMLButtonElement, Props>(function WeatherCard({ weather, options, now, ...props }, ref) {
  // Weather changes hourly; avoid rebuilding the chart during second ticks.
  const minute = Math.floor(now / 60000), timezone = options?.timezone, offset = options?.utcOffsetSeconds, supported = options?.timeZoneSupported;
  const model = useMemo(() => presentWeather(weather, timezone ? { timezone, utcOffsetSeconds: offset ?? 0, timeZoneSupported: supported } : null, minute * 60000),
    [weather, minute, timezone, offset, supported]);
  const gradient = `weather-${useId().replace(/:/g, '')}`;
  const location = weather?.location_label?.trim() || '地点未设置';
  const metadata = weather ? `${location}${weather.stale ? ' · 旧数据' : ''}${model.updated ? ` · ${model.updated} 更新` : ' · 更新时间未知'}` : '等待天气更新';
  const condition = weatherConditionInfo(weather?.condition_code, weather?.condition_text);
  return <RemoteButton {...props} className="today-card today-weather" aria-label="今日天气" title="按确定刷新简报" ref={ref}>
    <span className="today-card__heading"><span className="today-card__eyebrow today-card__eyebrow--weather">今日天气</span><span className="today-weather__updated">{metadata}</span></span>
    <span className="today-weather__current">
      <span className="today-weather__temperature" aria-label="当前温度">{model.temperature}</span>
      <span className="today-weather__description"><span className="today-weather__condition"><WeatherIcon code={weather?.condition_code} text={weather?.condition_text} />{weather ? condition.label : '暂无天气数据'}</span><span className="today-weather__range">{model.range}</span></span>
    </span>
    <span className="today-weather__stats">{model.stats.map(stat => <span className={`today-weather__stat${stat.label === '风' ? ' today-weather__stat--wind' : ''}`} key={stat.label}><span>{stat.label}</span><span className={stat.accent ? 'is-accent' : ''}>{stat.value}</span></span>)}</span>
    <span className="today-weather__hourly">
      {model.hourly.length ? <>
        <svg className="today-weather__chart" viewBox="0 0 436 104" role="img" aria-label="小时温度变化">
          <title>{model.hourly.map(point => `${point.time} ${Math.round(point.temperature)}°`).join('，')}</title>
          <defs><linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#edb563" stopOpacity=".42" /><stop offset="1" stopColor="#edb563" stopOpacity="0" /></linearGradient></defs>
          <path d={model.area} fill={`url(#${gradient})`} />
          <path d={model.line} fill="none" stroke="#edb563" strokeWidth="3.5" strokeLinecap="round" strokeLinejoin="round" />
          {model.hourly.map(point => <g key={point.timestamp}><text x={point.x} y={point.y - 13} textAnchor="middle" className={point.current ? 'is-current' : ''}>{Math.round(point.temperature)}°</text><circle cx={point.x} cy={point.y} r={point.current ? 8 : 5} /></g>)}
        </svg>
        <span className="today-weather__hours" aria-hidden="true">{model.hourly.map((point, index) => <span style={{ left: `${point.x / 436 * 100}%` }} key={point.timestamp}>{index % 2 === 0 || index === model.hourly.length - 1 ? point.time : ''}</span>)}</span>
      </> : <span className="today-weather__forecast-empty">暂无小时预报</span>}
    </span>
    <span className="today-weather__days" role="list" aria-label="未来天气">
      {model.days.length ? model.days.map(day => <span className={`today-weather__day${day.label === '今天' ? ' is-today' : ''}`} role="listitem" key={day.date} data-date={day.date}>
        <span className="today-weather__day-name">{day.label}</span><WeatherIcon code={day.code} />
        <span className="today-weather__low">{day.low}</span>
        <span className="today-weather__bar"><span style={{ left: `${day.left}%`, width: `${day.width}%` }} />{day.current !== null ? <span className="today-weather__now" style={{ left: `${day.current}%` }} /> : null}</span>
        <span className="today-weather__high">{day.high}</span>
      </span>) : <span className="today-weather__forecast-empty">暂无未来天气预报</span>}
    </span>
  </RemoteButton>;
});
