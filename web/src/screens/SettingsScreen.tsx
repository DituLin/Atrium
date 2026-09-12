import { useCallback, useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { nasWidget, photoWidget } from '../app/homeSelect';
import type { ScreenSelfResponse } from '../types/api';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { mapRemoteKey } from '../ui/keys';
import { RemoteButton } from '../ui/RemoteButton';
import { indexProgressText, nasStatusText, relativeTime, shareFreeText } from '../ui/statusText';

const TABS = ['连接状态', '照片来源', '关于 Atrium'] as const;

/** Only existing Core APIs; recovery rechecks without moving route or focus. */
export function SettingsScreen() {
  const { state, api, dispatch, clientVersion } = useApp();
  const [selected, setSelected] = useState(0);
  const [self, setSelf] = useState<ScreenSelfResponse | null>(null);
  const [notice, setNotice] = useState('正在检查…');
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const action = useRef<HTMLButtonElement>(null);
  const details = useRef<HTMLDivElement>(null);
  const currentRequest = useRef(0);
  const check = useCallback(async () => {
    const request = ++currentRequest.current;
    try {
      const [home, screen] = await Promise.all([api.getHome(), api.getScreenSelf()]);
      if (request !== currentRequest.current) return;
      dispatch({ type: 'app.homeLoaded', home, receivedAt: Date.now() });
      setSelf(screen); setNotice('已更新状态');
    } catch {
      if (request === currentRequest.current) setNotice('暂时无法更新 · 显示上次数据');
    }
  }, [api, dispatch]);
  useEffect(() => {
    const source = state.router.restoreFocus
      ? document.querySelector<HTMLElement>(`[data-return-focus="${state.router.restoreFocus}"]`) : null;
    (source ?? tabs.current[0])?.focus();
    let active = true;
    void Promise.resolve().then(() => { if (active) void check(); });
    return () => { active = false; currentRequest.current += 1; };
  }, [check, state.router.restoreFocus]);
  const sources = nasWidget(state.home)?.sources ?? [];
  const photo = photoWidget(state.home);
  return <div className="screen screen--settings">
    <Masthead />
    <h1 className="photos__title">设置与状态</h1>
    <main className="settings__layout">
      <div className="settings__tabs" role="tablist" aria-label="设置分类" aria-orientation="vertical">
        {TABS.map((label, index) => <RemoteButton key={label} role="tab" id={`settings-tab-${index}`}
          aria-selected={selected === index} aria-controls="settings-panel" ref={element => { tabs.current[index] = element; }}
          onClick={() => setSelected(index)} onDirection={key => {
            if (key === 'up') tabs.current[Math.max(0, index - 1)]?.focus();
            if (key === 'down') { if (index < 2) tabs.current[index + 1]?.focus(); else focusPrimaryNav('settings'); }
            if (key === 'right') action.current?.focus();
          }}>{label}</RemoteButton>)}
      </div>
      <section className="settings__panel" id="settings-panel" role="tabpanel" aria-labelledby={`settings-tab-${selected}`}>
        <div className="settings__body" ref={details} role="region" aria-label="状态详情" tabIndex={0} onKeyDown={event => {
          const key = mapRemoteKey(event);
          if (!key || key === 'back' || key === 'enter') return;
          event.preventDefault(); event.stopPropagation();
          if (key === 'left') tabs.current[selected]?.focus();
          if (key === 'right') action.current?.focus();
          if (key === 'up') event.currentTarget.scrollTop = Math.max(0, event.currentTarget.scrollTop - event.currentTarget.clientHeight * .7);
          if (key === 'down') {
            if (event.currentTarget.scrollTop + event.currentTarget.clientHeight >= event.currentTarget.scrollHeight - 1) action.current?.focus();
            else event.currentTarget.scrollTop += event.currentTarget.clientHeight * .7;
          }
        }}>
        <h2>{TABS[selected]}</h2>
        {selected === 0 ? <>
          <dl className="settings__facts">
            <div><dt>家庭服务</dt><dd>{state.connection.status === 'online' ? '已连接' : '连接未确认'}</dd></div>
            <div><dt>家庭</dt><dd>{state.home?.home.name ?? '尚未取得'}</dd></div>
            <div><dt>此屏幕</dt><dd>{self?.name ?? '尚未取得'}</dd></div>
            <div><dt>屏幕授权</dt><dd>{self?.status === 'active' ? '有效' : self?.status === 'revoked' ? '已撤销' : '尚未取得'}</dd></div>
            <div><dt>快照时间</dt><dd>{relativeTime(state.home?.server_time, state.nowMs)}</dd></div>
          </dl>
          <p>中枢连接状态仅表示家庭服务连接，不代表房屋设备状态。</p>
        </> : selected === 1 ? <>
          {sources.length ? sources.map(source => <div className="settings__source" key={source.id}>
            <h3>{source.name}</h3><p>{nasStatusText([source], state.nowMs).value}</p>
            <p>{nasStatusText([source], state.nowMs).detail}{shareFreeText([source]) ? ` · 共享空间${shareFreeText([source])}` : ''}</p>
          </div>) : <p>{nasWidget(state.home) ? '未配置照片来源' : '照片来源尚未取得'}</p>}
          <p>{photo ? `${photo.totals.ready} 张可展示 · ${photo.totals.pending_preview} 项预览待生成` : '照片数量尚未取得'}</p>
          {indexProgressText(photo) ? <p>{indexProgressText(photo)}</p> : null}
        </> : <>
          <p>留一方光景。</p>
          <dl className="settings__facts"><div><dt>客户端版本</dt><dd>{clientVersion}</dd></div></dl>
          <p>使用遥控器方向键移动，确认打开，返回上一级。</p>
        </>}
        </div>
        <div className="settings__recovery">
          <RemoteButton className="button" ref={action} onClick={() => { setNotice('正在检查…'); void check(); dispatch({ type: 'connection.retry' }); }}
            onDirection={key => { if (key === 'left') tabs.current[selected]?.focus(); if (key === 'up') details.current?.focus(); if (key === 'down') focusPrimaryNav('settings'); }}>重新检查</RemoteButton>
          <p className="settings__notice" role="status">{notice}</p>
          <p className="settings__help">↑ 查看详情，可上下滚动 · ← 返回分类。更改地址：在首页照片处按返回，选择「连接设置」。</p>
        </div>
      </section>
    </main>
    <PrimaryNav onUp={() => tabs.current[selected]?.focus()} />
  </div>;
}
