# Today page data sources

[中文](today-data.md) | English

This document describes the data behind Today’s almanac and weather. KOI calendar data is integrated. The server-side QWeather adapter and its UI are implemented; the real account and location still require configuration. See the verification status below and the [Today and House interface record](../tv-today-house-ui.en.md) for the previous interface release.

## Almanac

Today uses the generic lunar table, solar-term table and almanac rules from [KOI](https://github.com/DituLin/Koi), pinned to commit `f973da1b07a8bc3a387c202fd1a134f24924a3fc`. A pure TypeScript module computes lunar dates, year/month/day pillars, solar terms, the week strip and complete auspicious/inauspicious activity lists offline. It makes no external almanac requests and reads no KOI personal records. The page labels its source `KOI 黄历 · koi-almanac/1.0`.

The supported Gregorian range is **1901-01-01 through 2099-12-31**. The household timezone supplies the civil date; day-number arithmetic prevents the browser timezone or daylight saving from shifting it. Lunar years change at Chinese New Year. Almanac year pillars and zodiac change on the **civil date of Lichun**, and month pillars change on the civil dates of the twelve Jie terms, rather than at their exact transition times. Solar-term timestamps use KOI’s original UTC+8 table with minute precision; the progress bar uses civil days.

Activities follow KOI’s versioned, simplified traditional rules, including day-officer and deity rules, with prohibitions taking precedence in conflicts. They are not a publisher’s individually verified daily almanac. When a range boundary lacks a preceding or following term, the page retains an explicit missing state instead of mixing in approximate terms. Outside the range, Gregorian information remains visible with KOI’s supported range.

Original tables, rules and fixtures retain the source commit and SHA-256 hashes. The port is checked against KOI’s existing fixtures. Fixture conformance establishes agreement with KOI’s algorithm, rather than independent authoritative validation of every activity.

## QWeather

Core requests QWeather data and supplies the existing weather widget to the TV. The adapter uses the coordinate-based Weather v1 current, 24-hour and five-day endpoints. QWeather condition codes map to the existing WMO icon codes, with unknown conditions remaining unknown. Relative humidity converts from a fraction to percent and wind from m/s to km/h. Missing values or incompatible units do not become zeros. Failure of an optional forecast does not discard valid current conditions.

Set `widgets.weather.provider: qweather`, then configure the actual coordinates, location label and dedicated API host. An empty `qweather.timezone` uses `home.timezone`; explicitly configure the weather location's IANA timezone when it differs from the household's. The API host is a hostname without a scheme or path. All values below are example placeholders:

```yaml
widgets:
  weather:
    enabled: true
    provider: qweather
    latitude: 30.20
    longitude: 120.10
    location_label: Example district
    refresh_interval: 30m
    stale_after: 2h
    qweather:
      api_host: example.qweatherapi.com
      timezone: Asia/Shanghai
      api_key_file: /path/to/secrets/qweather-api-key
```

The API-key file contains one line, is owned by the Core user and has mode `0600`. Core authenticates through the `X-QW-Api-Key` header. Its contents never appear in configuration output or TV data. This file is separate from the existing `--provider-env-file`; do not append weather credentials to the file that only accepts the DeepSeek setting.

JWT is an alternative to an API key. Leave `api_key_file` empty and configure `developer_id`, `project_id`, `credential_id` and `private_key_file` under `qweather.jwt`. The private key is an Ed25519 PKCS8 PEM file owned by the Core user with mode `0600`. Core signs short-lived JWTs automatically, without storing a manually generated token that expires. See the [configuration example](../../config.example.yaml) and [official authentication documentation](https://dev.qweather.com/en/docs/configuration/authentication/).

The page displays QWeather attribution and the provider's source statements. Fetch time and observation time remain separate: an observation time is shown only when the response explicitly supplies it. Refresh time never substitutes for it. Cached data is bound to its provider and location, so changing configuration does not relabel an old provider's data as QWeather. Authentication failures and rate limits trigger request backoff. Retained readings follow their staleness rules, and missing forecasts retain empty states.

Actual activation requires a dedicated host, valid credential files and a confirmed weather location. Implemented endpoints and passing synthetic tests do not establish account authorization, quota or real-location acceptance. The adapter follows the current official [current weather](https://dev.qweather.com/en/docs/api/weather/weather-current/), [hourly](https://dev.qweather.com/en/docs/api/weather/weather-hourly-forecast/) and [daily](https://dev.qweather.com/en/docs/api/weather/weather-daily-forecast/) contracts.
