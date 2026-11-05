# Today page data sources

[中文](today-data.md) | English

This document describes the integration implemented in code. The November 5, 2026 KOI calendar enrichment and QWeather integration have not completed production configuration or acceptance. See the [Today and House interface record](../tv-today-house-ui.en.md) for the previous release.

## Almanac

The existing Gregorian/lunar dates, year/day sexagenary labels, solar terms and week strip use the household timezone. Auspicious/inauspicious activities and the month label require a verified almanac source, rather than values copied from a UI example. Until the actual KOI implementation is available, the page retains its missing states. A project specification is not a calendar database. Before reuse, verify its dependencies, license, year boundary, solar-term month changes and day-boundary convention, then document which data is reused.

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
