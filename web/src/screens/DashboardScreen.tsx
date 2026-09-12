import { useRef } from 'react';
import { useApp } from '../app/context';
import { showsReconnectingBanner } from '../app/connection';
import { nasWidget, photoWidget } from '../app/homeSelect';
import { WidgetBoundary } from '../ui/ErrorBoundary';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { RemoteButton } from '../ui/RemoteButton';
import { nasStatusText } from '../ui/statusText';
import { WidgetSlot } from '../widgets/registry';
import { PhotoPane } from '../widgets/PhotoPane';

export function DashboardScreen() {
  const { state, dispatch } = useApp();
  const status = useRef<HTMLButtonElement>(null);
  const hero = useRef<HTMLDivElement>(null);
  const focusPhoto = () => hero.current?.querySelector<HTMLElement>('[role="button"]')?.focus();
  const clock = state.home?.widgets.find(widget => widget.type === 'clock');
  const photo = photoWidget(state.home);
  const nas = nasStatusText(nasWidget(state.home)?.sources ?? [], state.nowMs);
  return <div className="screen screen--dashboard">
    <Masthead />
    {showsReconnectingBanner(state.connection) ? <div className="banner" role="status">正在重连家庭服务 · 显示上次数据</div> : null}
    <main className="dashboard">
      <div className="dashboard__main" ref={hero}>
        <WidgetBoundary name="photo"><PhotoPane payload={photo} interactive onDirection={key => {
          if (key === 'right') status.current?.focus();
          if (key === 'down') focusPrimaryNav('dashboard');
        }} /></WidgetBoundary>
      </div>
      <aside className="dashboard__side">
        {clock ? <WidgetSlot widget={clock} context={{ clock: state.clock, nowMs: state.nowMs }} /> : <p className="photos__note">时间尚未取得</p>}
        <div className="home__introduction"><h1>把日子，<br />留在眼前。</h1><p>确认照片，慢慢翻看。</p></div>
        <RemoteButton ref={status} className="home__status" aria-label="查看状态" onClick={() => dispatch({ type: 'router.navigate', route: { name: 'settings' } })}
          onDirection={key => { if (key === 'left') focusPhoto(); if (key === 'down') focusPrimaryNav('settings'); }}>
          <span aria-label="系统状态"><span>{state.connection.status === 'online' ? '● 家庭服务已连接' : '○ 家庭服务连接未确认'}</span><span>NAS · {nas.value}</span></span>
          <span>查看 ›</span>
        </RemoteButton>
      </aside>
    </main>
    <PrimaryNav onUp={focusPhoto} />
  </div>;
}
