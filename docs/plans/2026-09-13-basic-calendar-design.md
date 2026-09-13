# 普通月历实施设计

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按用户 9 月 13 日最新要求交付普通日历，不再以共享日历来源作为本轮前置条件。

**Architecture:** React 本地生成公历月历，复用 Core 校时与家庭时区、现有宋式 token、导航和返回栈。日期浏览不请求 NAS 或外部日历；远程导航白名单不扩展。

**Tech Stack:** React / TypeScript / Intl、现有 Go 路由上报契约；不增加依赖、账号或数据库。

## 设计与范围

七列（月曜至日曜）、固定六周的月历。突出今日，显示所浏览年月。只提供上个月、回到本月、下个月三个按钮，日期只读，不暗示可编辑日程。三按钮左右移动，确认执行，上下可回主导航；返回沿原页面恢复焦点。初始焦点在回到本月。浏览其他月份期间时钟更新不改变所浏览月份；本月模式跨月自动跟随，前台恢复立即更新今日。

沿用纸白青瓷、本地中文标题字体、细分隔线与充分留白。1080p、4K 和 OnePlus 804×384 都显示完整月份和固定操作/导航。家庭时区缺失时显示等待时区，不补造家庭日期；时钟超过现有核验期限显示时间未经核验。

本轮不显示节假日、农历、日程或提示已安排事件。今日页原“家庭日历”来源描述改为“日程同步”，保留未接入事实，普通月历本身可正常使用。

## 实施与验证

1. 新增 `web/src/app/calendar.ts` 及测试：闰年/世纪规则、月份天数、周起始、跨年、时区日期；先失败再实现。
2. 新增 `web/src/screens/CalendarScreen.tsx`、测试与 CSS：三个遥控按钮、本月恢复、时间更新、来源缺失；复用家庭时钟，稳定焦点。
3. `router.ts`、`state.ts`、`App.tsx`、`PrimaryNav.tsx`、`types/ws.ts` 与 Go/OpenAPI 同步本地 calendar 上报，保留远程 dashboard/photos 白名单；覆盖返回栈和权限优先级。
4. Web 全测试/lint/build，相关 Go 测试；三尺寸浏览器与 OnePlus 验证后按既有备份部署流程交付。

## NAS 视频范围澄清

图片和视频混放在既有 `/Volumes/home/Photos` 授权根，无需拆分目录，也不新增扫描根。视频索引按媒体类型区分并与照片共存。先前目录 open 阻塞仍是可读性问题，不能由混放推导为没有视频；可读后从该根选择真实样本并决定播放器方案。

## 交付状态

`fd30e88` 已部署至 Mac mini，OnePlus 遥控与刷新检查通过；完整记录见 [月历验收](../ops/song-tv-calendar-results.md)。
