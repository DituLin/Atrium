# TV redesign implementation (2026-09-26)

## 信息页

Scope: 今日、房屋、设置、配对、连接. All new CSS lives in `web/src/styles/info.css` under the `info-*` prefix so it cannot collide with the retired `.briefing__* / .house__* / .settings__* / .pair__* / .connect__*` rules still present in `styles.css`. Shared helpers are in `web/src/screens/info/` (`familyDate.ts`, `AlmanacCard.tsx`, `StatusRow.tsx`, `homeRows.ts`, `readingRegion.ts`).

Implemented:

- **今日** (`BriefingScreen`): paper `.page` with `TopNav`; heading `今日` + family date (`9月26日 · 星期六`) + `汇总于 HH:MM` from the snapshot. Three columns as in Today.dc.html:
  - 万年历 card (replaces "下一件事"; calendar is not connected): Gregorian day, year/month, weekday, `农历八月十六`, `丙午马年 · 癸卯日`, chips for `almanac.today` (festivals / solar term), `距寒露还有 N 天`. The family Y/M/D comes from `wallClockParts(serverNow(...))` in the home zone (clock widget first, then home / overview zone only when the engine supports it); without a trustworthy zone it prints 等待家庭时间 instead of a date. It rolls over at family midnight because it is derived from server time on every tick; `almanac()` is memoised per date.
  - 家庭提示: notice entries only, with 开始 / 有效至 / 更新于 / source label and 旧数据 when stale; empty state keeps 当前没有可展示的提示 plus an honest reason per availability.
  - 家里: rows for 中枢, each NAS source, 房屋资料, 环境数据, 日程同步 with ok / warn / hollow dots; 查看房屋 and 刷新简报 buttons. Refresh feedback moved to the footer hints row (`role=status`).
  - Focus: 今日事项 region (initial) ← → 万年历 card / 查看房屋 → 刷新简报; Up at the top edge of any of them → `focusTopNav()`; TopNav Down → region. Deadline, visibility withdrawal and `data-return-focus` behaviour unchanged.
- **房屋** (`HouseScreen`): heading + one-line summary computed from the cards (`N 项需要留意` / `中枢与照片来源运行正常` / `暂无需要留意的项目`). Left 中枢 region is a 2-column card grid (Core, 此屏幕, one card per NAS source, `card--warn` for anything abnormal) and stays a scrollable, focusable region. Right: 房间与资料 empty state (`房屋资料尚未填写`), 环境数据 row (`未接入`, hollow dot), 重新检查. Footer: feedback + `更新于 HH:MM:SS`.
- **设置** (`SettingsScreen`): left category tablist, right hairline rows with label / help / value. Only the three existing categories (连接状态 / 照片来源 / 关于 Atrium) and existing facts; recheck flow unchanged. First category Up → TopNav.
- **配对** (`PairScreen`): two-column Pair.dc.html layout, code split 3 + 3 at 168 px, three steps with the real `atrium admin pair approve <code> --name "客厅屏幕"`, progress bar + `m:ss` countdown to `expires_at`. Polling / expiry restart / claim in `usePairing` untouched. The command never shows an expired code.
- **连接** (`ConnectScreen`): ink layout after Offline.dc.html: status pill (lead + consecutive failures), family clock bottom-left only when a clock widget gives a trustworthy zone, panel with explanation, facts (连接 / 连续失败 / 上次断开代码) and 立即重试 / 重新连接此屏幕 + 查看中枢状态. Superseded and auth-expired variants kept.

Deviations from the artboards:

- Today: "下一件事" hero and upcoming list replaced by the 万年历 card (user request; calendar not connected). 家里 rows are overview sources (中枢 / NAS / 房屋资料 / 环境数据 / 日程同步) rather than the mock's 照片库 count / 客厅电视, which the overview does not carry. Refresh button added next to 查看房屋 (the artboard has none).
- House: the artboard's "照片处理 · 4 张暂不能展示" and "已连续运行 20 天" are not in the House API, so they are not shown. Footer says `更新于` instead of "每 30 秒自动更新".
- Settings: 显示与轮播 / 此屏幕 categories and the adjustable ‹ › rows are omitted — the client cannot apply those settings. Footer hint is ↑↓ 选分类 / → 查看详情 instead of ← → 调整. The old hint "更改地址：在首页照片处按返回，选择「连接设置」" was removed because the redesigned home no longer has that entry.
- Connect: no background photo (this screen is shown exactly when there is nothing authorised to display); the family clock appears only when the zone is known.

Verification: `npx tsc -b --noEmit` clean; `npx eslint src` clean for these files; `npx vitest run` and `npm run build` pass for the information pages (see the implementation report for the full-suite status at the time).

## 影像

Scope: photo library (`PhotosScreen` / `PhotoGrid`), photo preview (`PhotoScreen`), video list (`VideosScreen`) and player (`video/VideoPlayer`). All CSS lives in `web/src/styles/media.css`; the old `screens/videos.css` and `video/video-player.css` were removed. Shared pieces are in `web/src/screens/media/` (`MediaRail.tsx`, `libraryLayout.ts`, `viewerPosition.ts`, `videoFacts.ts`), each pure helper with its own test.

Implemented:

- **影像库** (photos and videos routes): ink page, shared `TopNav`, left rail (360u) `影像` with 最近新增 · 今天拍摄 · 随心看看 · 全部照片 · 视频 (`MediaRail`, `role=tab`, vertical). Both routes render the same rail with the right entry selected; 视频 navigates to `videos`, a collection entry navigates to `photos`. Focusing an entry never applies it; OK does (unchanged semantics). Right column: order line from the API contract (`按拍摄时间，最新在前` …), count (`共 N 张` only when `meta.total` exists, otherwise `已载入 N(+) 张`), notices, then a uniform 4-column grid of 16:10 cover tiles (3 columns at ≤900 px as before). Focused tile scales 1.06 with a celadon-light outline and a date caption overlay; video tiles carry a play glyph + duration badge. `全部照片` (the only capture-ordered collection) is grouped by capture month in the home timezone with `拍摄时间未知` last; other collections stay flat. Empty/failed states keep the existing wording and recovery actions. Footer `.hints`.
- **Remote** in the library: rail Up/Down, Up from the first entry → TopNav, Right → content; grid Left from column 0 → rail, Up from the first row → TopNav, Down at the bottom stays (no bottom nav anymore). Across month sections Up/Down keep the column and clamp into a shorter row; inside a section the flat-grid rule (short tail keeps focus) is unchanged. Videos: top row Up → 刷新/重试, which goes Up → TopNav and Left → rail. TopNav Down → selected rail entry.
- **照片预览**: ink-deep full-bleed, photo `object-fit: contain`, `返回 · 集合` top-left, non-focusable side chevrons (dimmed at the ends), a hint row when the layer is hidden. OK opens the operation layer: capture date (+估算), `集合 · 第 x / y 张` (y only when the list is fully loaded or `meta.total` exists; nothing when opened alone), a ±2 neighbour filmstrip from the frozen client sequence (failed photos skipped), and the existing actions 关闭操作 / 上一张 / 下一张 / 返回来源. Back closes the layer first, then returns. `onload` render signal, retained image, 202/404/503 handling untouched.
- **视频播放**: header with `影像 NN` and `视频 · 第 x (/ y) 段` from the list position, `音量请用遥控器调节`; bottom scrim, custom progress track (played fill + thumb, `role=progressbar`), elapsed/total times, round moon play/pause/replay button (accessible name unchanged: 播放/暂停/重新播放/重试播放), hint text, `1080p · H.264` when metadata exists, and 返回视频. Playback lifecycle, ±10 s seeking and error states unchanged.
- **Router** (minimal, tested): switching between `photos` and `videos` no longer pushes a return layer, so Back from the library leaves the library in one step whichever rail entry is selected.

Deviations from the artboards:

- 全部/照片/视频 filter chips omitted (no merged endpoint); the rail's 视频 entry replaces them. Rail says 全部照片 (photos only) instead of 全部影像.
- Rail counts: only the selected collection, and only when the API returns `meta.total`; month headers carry no `N 项` (the client only knows loaded items).
- Library has the shared TopNav above it (the artboard has none) so every page stays reachable; the rail's inline hint moved into the footer `.hints` row.
- Photo layer: no 从这张开始轮播 / 详情 buttons (no such actions exist); the filmstrip shows ±2 neighbours (not ±3) so it fits beside the four existing actions; it is display-only.
- Video player: no 上一段/下一段 buttons and no "preview target time, OK to jump" scrubber — Left/Right keep the existing immediate ±10 s seek, which would make side buttons unreachable. No capture date in the header (videos have none in the API).
- Photo preview's old pointer buttons (照片操作 / 返回) in the footer were dropped; OK and Back cover them.
