import { VideosScreen } from './screens/VideosScreen';
import { BriefingScreen } from './screens/BriefingScreen';
/**
 * Screen switch plus the global error boundary. A crash anywhere below lands
 * on the connect screen with a retry instead of a white page (W-106, FR-03).
 */

import type { ReactElement } from 'react';

import { AppProvider } from './app/AppProvider';
import { useApp } from './app/context';
import { selectScreen } from './app/state';
import { ConnectScreen } from './screens/ConnectScreen';
import { DashboardScreen } from './screens/DashboardScreen';
import { PairScreen } from './screens/PairScreen';
import { PhotoScreen } from './screens/PhotoScreen';
import { HouseScreen } from './screens/HouseScreen';
import { SettingsScreen } from './screens/SettingsScreen';
import { PhotosScreen } from './screens/PhotosScreen';
import { ErrorBoundary } from './ui/ErrorBoundary';

export function CurrentScreen(): ReactElement {
  const { state } = useApp();
  switch (selectScreen(state)) {
    case 'pair':
      return <PairScreen />;
    case 'connect':
      return <ConnectScreen />;
    case 'briefing':
      return <BriefingScreen />;
    case 'house':
      return <HouseScreen />;
    case 'settings':
      return <SettingsScreen />;
    case 'videos':
    case 'video':
      return <VideosScreen />;
    case 'photos':
      return <PhotosScreen />;
    case 'photo':
      return <PhotoScreen />;
    case 'dashboard':
    default:
      return <DashboardScreen />;
  }
}

function GlobalFallback(props: { error: Error; reset: () => void }): ReactElement {
  return (
    <div className="page surface--ink app-fallback">
      <p className="app-fallback__lead" role="status"><span className="dot dot--warn" /> 画面暂时无法显示</p>
      <p className="app-fallback__detail muted">{props.error.message}</p>
      <button type="button" className="btn btn--moon" onClick={props.reset}>重试</button>
    </div>
  );
}

export function App(): ReactElement {
  return (
    <ErrorBoundary fallback={(error, reset) => <GlobalFallback error={error} reset={reset} />}>
      <AppProvider>
        <CurrentScreen />
      </AppProvider>
    </ErrorBoundary>
  );
}
