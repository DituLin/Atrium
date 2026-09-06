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
import { PhotosScreen } from './screens/PhotosScreen';
import { ErrorBoundary } from './ui/ErrorBoundary';

export function CurrentScreen(): ReactElement {
  const { state } = useApp();
  switch (selectScreen(state)) {
    case 'pair':
      return <PairScreen />;
    case 'connect':
      return <ConnectScreen />;
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
    <div className="screen screen--connect">
      <h1 className="connect__title">Atrium</h1>
      <p className="connect__lead" role="status">
        <span aria-hidden="true">■</span> The screen stopped rendering
      </p>
      <p className="connect__detail">{props.error.message}</p>
      <button type="button" className="button" onClick={props.reset}>
        Retry
      </button>
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
