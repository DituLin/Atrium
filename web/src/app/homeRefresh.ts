import type { ApiClient } from '../core/api';
import type { AppAction } from './state';

// A screen's lifecycle, socket commands, reconnect preflight and Settings all
// share one API. Their responses must share ordering too, regardless of owner.
const orders = new WeakMap<ApiClient, { next: number; applied: number }>();

export async function refreshHomeSnapshot(ports: {
  api: ApiClient;
  dispatch: (action: AppAction) => void;
  now?: () => number;
  isCurrent?: () => boolean;
}): Promise<void> {
  let order = orders.get(ports.api);
  if (!order) { order = { next: 0, applied: 0 }; orders.set(ports.api, order); }
  const request = ++order.next;
  // ApiClient rejects responses that cross an authorization generation.
  const home = await ports.api.getHome();
  if (request < order.applied || (ports.isCurrent && !ports.isCurrent())) return;
  order.applied = request;
  ports.dispatch({ type: 'app.homeLoaded', home, receivedAt: (ports.now ?? Date.now)() });
}
