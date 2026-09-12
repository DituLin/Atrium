import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { initialConnectionState } from '../app/connection';
import type { ConnectionState } from '../app/connection';
import { StatusBar } from './StatusBar';

function renderStatus(connection: Partial<ConnectionState>) {
  return render(<StatusBar connection={{ ...initialConnectionState, ...connection }}
    nas={null} photo={null} homeName="家" clientVersion="1.0" nowMs={0} />);
}

describe('Chinese connection status', () => {
  it.each(['offline', 'reconnecting', 'connecting', 'idle'] as const)(
    'does not claim current reachability from a saved snapshot while %s', (status) => {
      renderStatus({ status, hasSnapshot: true });
      const core = screen.getByText('家庭服务').parentElement as HTMLElement;
      expect(within(core).queryByText('已连接')).toBeNull();
      expect(within(core).getByText('连接未确认')).toBeDefined();
      expect(within(core).getByText('显示上次数据')).toBeDefined();
    },
  );

  it('reports a current connection only when online', () => {
    renderStatus({ status: 'online', hasSnapshot: true });
    const core = screen.getByText('家庭服务').parentElement as HTMLElement;
    expect(within(core).getByText('已连接')).toBeDefined();
    expect(screen.queryByText('显示上次数据')).toBeNull();
  });

  it('does not pretend an initial connection attempt is a confirmed failure', () => {
    renderStatus({ status: 'connecting' });
    expect(screen.getByText('正在连接')).toBeDefined();
    expect(screen.getByText('尚未取得数据')).toBeDefined();
  });

  it('explains superseded sessions in Chinese', () => {
    renderStatus({ status: 'offline', hasSnapshot: true, stopReason: 'superseded' });
    expect(screen.getByText('另一会话已接管')).toBeDefined();
  });
});
