import { useEffect } from 'react';
import { useApp } from '../app/context';
import { nasWidget, photoWidget } from '../app/homeSelect';
import { Masthead, PrimaryNav, focusPrimaryNav } from '../ui/PrimaryNav';
import { StatusBar } from '../ui/StatusBar';

/** Read-only information from the existing snapshot; connection editing is native. */
export function SettingsScreen() {
  const { state, clientVersion } = useApp();
  useEffect(() => { focusPrimaryNav('settings'); }, []);
  return <div className="screen screen--settings">
    <Masthead />
    <main className="settings__content">
      <h1 className="photos__title">设置与状态</h1>
      <p className="settings__intro">查看家庭服务、照片来源与此屏幕的信息。</p>
      <StatusBar connection={state.connection} nas={nasWidget(state.home)} photo={photoWidget(state.home)}
        homeName={state.home?.home.name ?? 'Atrium'} clientVersion="" nowMs={state.nowMs} />
      <dl className="settings__facts"><div><dt>客户端版本</dt><dd>{clientVersion}</dd></div></dl>
    </main>
    <PrimaryNav onUp={() => focusPrimaryNav('settings')} />
  </div>;
}
