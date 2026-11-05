# Growth curve and dashboard

[中文](growth-dashboard.md) · [Documentation index](../README.md)

Select a person on the TV **Archives** page. A full-width growth curve appears below milk and archive composition, with height/weight controls. The existing height, weight and finance cards remain below it. Statistics cover that person's authorized measurement history independently of recent-archive pagination.

For a selected person, the milk heading uses their existing birth date to show current calendar age: “奶量 - 6个月9天” below one year, and “奶量 - 1岁2个月” from one year onward, retaining “0个月” at an exact year. Age uses the server-synchronized household date. Month anniversaries clamp the original birth day to the corresponding month’s end; residual days below one year start at the most recent anniversary. The label updates at household midnight and foreground return. Missing birth date, identity or trusted time leaves the heading as “奶量”; household, property and other non-person scopes retain their scope labels.

Submit measurements with explicit dates, values and units through the existing IM or archive intake. Attributed, TV-visible `height`, 身高, 身长, `weight` and 体重 observations update with archive verification. Units, currencies and samples remain separate. Original values retain their precision; missing days are not zero-filled or predicted. The curve connects measured points. Latest values and changes come from records; “recorded growth over the last three months” compares available observations within three calendar months ending at the latest reading and displays the actual dates.

## WHO reference

Without configured profile information the X axis uses calendar dates. Once a full birth date and sex are confirmed and the person belongs to a complete public archive snapshot, the chart can show age in months, the WHO P50 median and P3–P97 band. Complete measurements also enable the difference from the same-age median, percentile and P50 change over the identical observed interval. Without measurements, only reference lines and current age appear; measured values, growth and percentile remain empty.

Profiles stay in private host configuration. This example is entirely synthetic. Replace the entity ID only with a person confirmed for TV display; the user must also confirm the birth date, sex and measurement posture:

```yaml
archives:
  growth_profiles:
    - entity_id: "synthetic_child"
      birth_date: "2025-01-01"
      sex: "male"
      height_measurement: "length"
```

`sex` is `male` or `female`. `height_measurement` is recumbent length (`length`) or standing height (`height`), and may be omitted if unconfirmed. At most 16 profiles are supported, with unique entity IDs. Restart Core through the existing deployment process after changing configuration. Redacted configuration output excludes profiles. Private, unmatched or incomplete public projections never return this information.

WHO parameters cover ages 0–1856 days; the month axis divides age in days by 30.4375. Length/height standards are segmented at day 731. When confirmed posture differs from the age-specific standard, reference values follow the 0.7 cm conversion with an explicit note; original measurements remain unchanged. Supported units are `cm`, `m`, `mm`, `kg`, `g` and their Chinese equivalents. Unknown posture disables length/height percentiles and median differences. Missing information, unsupported age/units, pre-birth history or incompletely verified measurements suspend the corresponding comparisons.

Reference parameters ship offline with the frontend; viewing the chart sends no personal information to external services. Percentiles appear only within the range supported by WHO's method. P3–P97 describes reference position, without a “normal” diagnosis or prediction of personal height/weight. “Standard change over the same interval” is the difference between two P50 values, not WHO growth-velocity standards. See [WHO data attribution and methodology](../attributions/who-growth.en.md) for sources, hashes and calculations.

## Remote control

Move down from milk period controls to the growth indicator controls. Use left/right and confirm to switch height/weight. Up returns to milk. Down or the right boundary enters the reading region, where up/down scroll through the chart. Right enters a currently visible control; left returns to the person list. Returning from a detail restores the original archive-card position. Person changes, revocation, verification failures and expiry clear prior measurements and reference information.

## Age heading release status (2026-11-05)

The age heading is deployed with frontend `86fb0b7` and verified on OnePlus. The displayed age matches the selected person's existing date of birth and the server household date; the milk heading no longer includes the person's name. The day, week and month controls retain their layout beside the heading. Birthday, month-end, leap-year, midnight and foreground-resume behavior is tested, with 9,675 synthetic dates checked against an independent calendar algorithm. Headings below and above one year also passed layout checks at widths of 1920 and 804.
