/**
 * Dashboard (FR-01). Fixed layout designed at 1920×1080 and scaled with vw/vh
 * so a 4K panel is the same composition at twice the pixels. Order is server
 * driven; unknown widget types disappear silently.
 */

import type { ReactElement } from 'react';
import { useCallback } from 'react';

import { useApp } from '../app/context';
import { showsReconnectingBanner } from '../app/connection';
import { nasWidget, photoWidget } from '../app/homeSelect';
import { DEFAULT_COLLECTION } from '../app/router';
import { currentSlide } from '../app/slideshow';
import { useRemoteKeys } from '../app/useTick';
import type { Widget } from '../types/api';
import { StatusBar } from '../ui/StatusBar';
import { WidgetBoundary } from '../ui/ErrorBoundary';
import { WidgetSlot } from '../widgets/registry';
import { PhotoPane } from '../widgets/PhotoPane';

const SIDE_TYPES = new Set(['clock', 'weather', 'notice']);

export function DashboardScreen(): ReactElement {
  const { state, dispatch, clientVersion } = useApp();

  // Remote entry points (design §7.1, PRD 4.2): Left/Right open the collection
  // browser, Enter opens the photo currently on the pane.
  useRemoteKeys(
    useCallback(
      (key) => {
        if (key === 'left' || key === 'right') {
          dispatch({
            type: 'router.navigate',
            route: { name: 'photos', collection: DEFAULT_COLLECTION },
          });
          return;
        }
        if (key !== 'enter') return;
        const slide = currentSlide(state.slideshow);
        if (slide) {
          dispatch({ type: 'router.navigate', route: { name: 'photo', photoId: slide.id } });
        }
      },
      [dispatch, state.slideshow],
    ),
  );

  const widgets: readonly Widget[] = state.home?.widgets ?? [];
  const context = { clock: state.clock, nowMs: state.nowMs };
  const photo = photoWidget(state.home);
  const nas = nasWidget(state.home);
  const side = widgets.filter((widget) => SIDE_TYPES.has(widget.type));

  return (
    <div className="screen screen--dashboard">
      {showsReconnectingBanner(state.connection) ? (
        <div className="banner" role="status">
          <span aria-hidden="true">↻</span> Reconnecting to Atrium Core — showing the last known
          state
        </div>
      ) : null}

      <main className="dashboard">
        <div className="dashboard__side">
          {side.map((widget, index) => (
            <WidgetSlot key={`${widget.type}-${index}`} widget={widget} context={context} />
          ))}
        </div>
        <div className="dashboard__main">
          <WidgetBoundary name="photo">
            <PhotoPane payload={photo} />
          </WidgetBoundary>
        </div>
      </main>

      <StatusBar
        connection={state.connection}
        nas={nas}
        photo={photo}
        homeName={state.home?.home.name ?? 'Atrium'}
        clientVersion={clientVersion}
        nowMs={state.nowMs}
      />
    </div>
  );
}
