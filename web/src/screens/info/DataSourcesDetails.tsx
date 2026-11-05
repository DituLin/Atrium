import { KOI_SOURCE } from '../../core/koi';
import type { WeatherWidgetPayload } from '../../types/api';
import { WeatherAttribution } from './WeatherAttribution';

/** One product-level source record; ordinary pages keep their content concise. */
export function DataSourcesDetails({ weather }: { weather: WeatherWidgetPayload | null }) {
  return <div className="settings-data-sources">
    <h3>数据来源</h3>
    <section className="settings-data-sources__item" aria-label="万年历来源">
      <h4>万年历</h4><p>锦鲤万年历（KOI）</p>
      <p className="settings-data-sources__note">农历、干支、节气与宜忌 · {KOI_SOURCE.ruleVersion} · 支持 1901—2099</p>
      <a href={KOI_SOURCE.repository} target="_blank" rel="noopener noreferrer" tabIndex={-1} onMouseDown={event => event.preventDefault()}>项目来源</a>
    </section>
    <section className="settings-data-sources__item" aria-label="天气来源"><h4>天气</h4>
      {weather?.provider === 'qweather' ? <WeatherAttribution provider={weather.provider} attributions={weather.attributions} readingMode="parent" />
        : weather?.provider === 'open-meteo' ? <a href="https://open-meteo.com/" target="_blank" rel="noopener noreferrer" tabIndex={-1} onMouseDown={event => event.preventDefault()}>Open-Meteo</a>
          : <p>{weather ? '天气来源待确认' : '天气来源尚未取得'}</p>}
    </section>
  </div>;
}
