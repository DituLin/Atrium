import { useCallback, useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { nasWidget, photoWidget } from '../app/homeSelect';
import type { ScreenSelfResponse } from '../types/api';
import { RemoteButton } from '../ui/RemoteButton';
import { TopNav, focusTopNav } from '../ui/TopNav';
import { indexProgressText, nasStatusText, relativeTime, shareFreeText } from '../ui/statusText';
import { StatusRow } from './info/StatusRow';
import { onReadingKey } from './info/readingRegion';

const TABS = ['连接状态', '照片来源', '关于 Atrium'] as const;
const NAS_TONE = { online: 'ok', degraded: 'warn', offline: 'warn', unknown: 'none' } as const;

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
  const nas = nasWidget(state.home);
  const sources = nas?.sources ?? [];
  const photo = photoWidget(state.home);
  const online = state.connection.status === 'online';
  const indexing = indexProgressText(photo);
  return <div className="page info-settings">
    <TopNav onDown={() => tabs.current[selected]?.focus()} />
    <div className="page__heading"><h1 className="page__title">设置</h1><span className="page__subtitle">状态与信息</span></div>
    <main className="page__body info-settings__grid">
      <div className="info-settings__tabs" role="tablist" aria-label="设置分类" aria-orientation="vertical">
        {TABS.map((label, index) => <RemoteButton key={label} role="tab" id={`settings-tab-${index}`} className="info-settings__tab"
          aria-selected={selected === index} aria-controls="settings-panel" ref={element => { tabs.current[index] = element; }}
          onClick={() => setSelected(index)} onDirection={key => {
            if (key === 'up') { if (index > 0) tabs.current[index - 1]?.focus(); else focusTopNav(); }
            if (key === 'down' && index < TABS.length - 1) tabs.current[index + 1]?.focus();
            if (key === 'right') action.current?.focus();
          }}>{label}</RemoteButton>)}
      </div>
      <section className="info-settings__panel" id="settings-panel" role="tabpanel" aria-labelledby={`settings-tab-${selected}`}>
        <div className="info-settings__body" ref={details} role="region" aria-label="状态详情" tabIndex={0} onKeyDown={event => onReadingKey(event, {
          left: () => tabs.current[selected]?.focus(), right: () => action.current?.focus(),
          top: () => focusTopNav(), bottom: () => action.current?.focus(),
        })}>
          <h2 className="page__section-title">{TABS[selected]}</h2>
          {selected === 0 ? <div className="row-list">
            <StatusRow tone={online ? 'ok' : 'none'} label="家庭服务" help="仅表示家庭服务连接，不代表房屋设备状态" value={online ? '已连接' : '连接未确认'} />
            <StatusRow tone={state.home ? 'ok' : 'none'} label="家庭" value={state.home?.home.name ?? '尚未取得'} />
            <StatusRow tone={self ? 'ok' : 'none'} label="此屏幕" value={self?.name ?? '尚未取得'} />
            <StatusRow tone={self?.status === 'active' ? 'ok' : self?.status === 'revoked' ? 'warn' : 'none'} label="屏幕授权"
              value={self?.status === 'active' ? '有效' : self?.status === 'revoked' ? '已撤销' : '尚未取得'} />
            <StatusRow tone={state.home ? 'ok' : 'none'} label="快照时间" value={relativeTime(state.home?.server_time, state.nowMs)} />
            <StatusRow label="更改服务地址" help="回到首页，在照片上按返回，选择「连接设置」" value="" />
          </div> : selected === 1 ? <div className="row-list">
            {sources.length ? sources.map(source => {
              const text = nasStatusText([source], state.nowMs);
              const free = shareFreeText([source]);
              return <StatusRow key={source.id} tone={NAS_TONE[text.health]} label={source.name} help={`${text.detail}${free ? ` · 共享空间${free}` : ''}`} value={text.value} />;
            }) : <StatusRow tone="none" label="照片来源" value={nas ? '未配置照片来源' : '照片来源尚未取得'} />}
            <StatusRow tone={photo ? 'ok' : 'none'} label="照片数量" help={photo ? `${photo.totals.pending_preview} 项预览待生成` : undefined}
              value={photo ? `${photo.totals.ready} 张可展示` : '照片数量尚未取得'} />
            {indexing ? <StatusRow tone="warn" label="索引" value={indexing} /> : null}
          </div> : <div className="row-list">
            <p className="info-settings__motto serif">留一方光景。</p>
            <StatusRow tone="ok" label="客户端版本" value={clientVersion} />
            <StatusRow label="遥控器" help="方向键移动，确认打开，返回上一级" value="" />
          </div>}
        </div>
        <div className="info-actions">
          <RemoteButton className="btn" ref={action} onClick={() => { setNotice('正在检查…'); void check(); dispatch({ type: 'connection.retry' }); }}
            onDirection={key => { if (key === 'left') tabs.current[selected]?.focus(); if (key === 'up') details.current?.focus(); }}>重新检查</RemoteButton>
        </div>
      </section>
    </main>
    <footer className="hints">
      <span>↑↓ 选分类</span><span>→ 查看详情</span><span>返回 回到首页</span>
      <span className="hints__end" role="status">{notice}</span>
    </footer>
  </div>;
}
