/**
 * Builds the screen's `ApiClient` and connects its auth callbacks to the
 * reducer. Kept outside the React tree so the impure bits (`Date.now`,
 * localStorage) never run during a render.
 */

import { ApiClient } from '../core/api';
import { saveLastAuthOkAt } from '../core/authExpiry';
import type { AppAction } from './state';

export function createScreenApi(dispatch: (action: AppAction) => void): ApiClient {
  return new ApiClient({
    onAuthOk: () => {
      saveLastAuthOkAt(Date.now());
      dispatch({ type: 'app.authOk' });
    },
    onUnauthorized: () => {
      dispatch({ type: 'app.needsPairing' });
    },
  });
}
