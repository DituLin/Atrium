import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it } from 'vitest';
import type { HouseIoT } from '../../types/api';
import { IoTObservations } from './IoTObservations';
function fixture(): HouseIoT {
  const observation = { value: null, availability: 'available' as const, source_updated_at: null, sampled_at: null, received_at: '2026-11-04T10:00:00Z', confirmed_at: '2026-11-04T10:01:00Z' };
  return { availability: 'available', reason: null, instances: [{ id: 'demo', provider: 'home_assistant', status: 'connected', generation: 1, last_success_at: observation.confirmed_at, devices: [
    { id: 'demo-humidifier', name: '示例加湿器', room_id: 'demo-living', room_name: '示例客厅', capabilities: [{ id: 'power', name: '电源', kind: 'power', observation: { ...observation, value: false } }, { id: 'current', name: '', kind: 'humidity', observation: { ...observation, value: 0, unit: '%' } }, { id: 'target', name: '', kind: 'humidity', observation: { ...observation, value: 55, unit: '%' } }] },
    { id: 'demo-thermometer', name: '示例温湿度计', room_id: '', room_name: '', capabilities: [{ id: 'temperature', name: '室温', kind: 'temperature', observation: { ...observation, availability: 'unknown' } }, { id: 'battery', name: '电量', kind: 'battery', observation: { ...observation, availability: 'unavailable' } }] },
  ] }] };
}
it('shows every authorized capability without coercing false/zero or combining current and target humidity', () => {
  render(<IoTObservations iot={fixture()} stale={false} timezone="UTC" />);
  expect(screen.getByText('关闭')).toBeDefined(); expect(screen.getByText('0 %')).toBeDefined();
  expect(screen.getByText('湿度 · current')).toBeDefined(); expect(screen.getByText('湿度 · target')).toBeDefined();
  expect(screen.getByText('55 %')).toBeDefined(); expect(screen.getByText('未知')).toBeDefined(); expect(screen.getByText('不可用')).toBeDefined();
  expect(screen.getByRole('heading', { name: '示例客厅' })).toBeDefined(); expect(screen.getByRole('heading', { name: '未分配房间' })).toBeDefined();
  expect(screen.queryByRole('switch')).toBeNull(); expect(screen.queryByRole('button')).toBeNull();
});
it('keeps receipt, confirmation, source-change and sampling timestamps distinct and never claims recently measured', () => {
  render(<IoTObservations iot={fixture()} stale={false} timezone="UTC" />);
  expect(screen.getAllByText('来源变更').length).toBeGreaterThan(0); expect(screen.getAllByText('测量时间').length).toBeGreaterThan(0);
  expect(screen.getAllByText('接收时间').length).toBeGreaterThan(0); expect(screen.getAllByText('同步确认').length).toBeGreaterThan(0);
  expect(screen.getAllByText('未提供').length).toBeGreaterThan(0); expect(screen.queryByText('刚测量')).toBeNull();
  expect(screen.getByText('最近同步')).toBeDefined();
});
it('distinguishes first sync, failure and stale disconnected observations and clears removed authorizations without stealing focus', () => {
  const value = fixture(); const view = render(<IoTObservations iot={{ ...value, availability: 'loading', instances: [] }} stale={false} timezone="UTC" />);
  expect(screen.getByText('设备状态等待读取')).toBeDefined();
  view.rerender(<IoTObservations iot={{ ...value, availability: 'failed', instances: [] }} stale={false} timezone="UTC" />);
  expect(screen.getByText('暂时无法读取设备状态')).toBeDefined();
  view.rerender(<IoTObservations iot={{ ...value, instances: value.instances.map(instance => ({ ...instance, status: 'disconnected' })) }} stale={true} timezone="UTC" />);
  expect(screen.getByText('设备状态待更新 · 显示上次观测')).toBeDefined(); expect(screen.getByText('Home Assistant · 未连接')).toBeDefined();
  const region = screen.getByRole('region', { name: '已接入设备' }); region.focus();
  expect(within(region).getAllByText('上次观测').length).toBeGreaterThan(0);
  view.rerender(<IoTObservations iot={{ ...value, instances: [] }} stale={false} timezone="UTC" />);
  expect(screen.queryByText('示例加湿器')).toBeNull(); expect(document.activeElement).toBe(region);
  expect(screen.getByText('没有可展示的设备')).toBeDefined();
});
it('scrolls a long read-only observation region with the remote without clicking a device', () => {
  render(<IoTObservations iot={fixture()} stale={false} timezone="UTC" />);
  const region = screen.getByRole('region', { name: '已接入设备' });
  Object.defineProperties(region, { clientHeight: { value: 100 }, scrollHeight: { value: 300 } });
  region.focus(); fireEvent.keyDown(region, { key: 'ArrowDown' }); expect(region.scrollTop).toBe(70);
  fireEvent.keyDown(region, { key: 'ArrowUp' }); expect(region.scrollTop).toBe(0);
});
