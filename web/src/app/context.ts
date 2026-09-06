/**
 * React context for the app state tree. Kept in its own module so the provider
 * component and the hooks can be imported without a cycle.
 */

import { createContext, useContext } from 'react';

import type { ApiClient } from '../core/api';
import type { AppAction, AppState } from './state';

export interface AppContextValue {
  state: AppState;
  dispatch: (action: AppAction) => void;
  api: ApiClient;
  clientVersion: string;
  /** Remote/back handling lives in the provider so every screen shares it. */
  goBack: () => void;
}

export const AppContext = createContext<AppContextValue | null>(null);

export function useApp(): AppContextValue {
  const value = useContext(AppContext);
  if (!value) throw new Error('useApp must be used inside <AppProvider>');
  return value;
}
