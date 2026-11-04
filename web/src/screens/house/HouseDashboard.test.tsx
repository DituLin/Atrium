import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it } from 'vitest';
import { HouseDashboard } from './HouseDashboard';
import type { HouseDashboardModel } from './model';

/** Synthetic room and device observations; never imported by the production adapter. */
function fixture(): HouseDashboardModel {
  const fresh = (value: number) => ({ value, state: 'available' as const });
  const history = { current: fresh(23.4), endAt: '2026-11-04T20:00:00Z', samples: [{ at: '2026-11-04T18:00:00Z', value: 22.8 }, { at: '2026-11-04T19:00:00Z', value: null }, { at: '2026-11-04T20:00:00Z', value: 23.4 }] };
  return { rooms: [
    { id: 'demo-one', name: '示例一区', rect: { x: 0, y: 0, width: 560, height: 640 }, temperature: fresh(23.4), humidity: fresh(54) },
    { id: 'demo-two', name: '示例二区', rect: { x: 560, y: 0, width: 560, height: 640 }, temperature: { value: 22.1, state: 'stale' }, humidity: fresh(52) },
  ], devices: [{ id: 'demo-light', name: '示例灯', kind: 'light', roomId: 'demo-one', state: 'on', description: '亮度 60%', observation: 'available', position: { x: 280, y: 300 }, brightnessPercent: 60 }, { id: 'demo-ac', name: '示例空调', kind: 'ac', roomId: 'demo-two', state: 'off', description: '待机', observation: 'available' }], temperature: history, humidity: { ...history, current: fresh(54), samples: history.samples.map(sample => ({ ...sample, value: sample.value === null ? null : 54 })) }, aquarium: { name: '示例鱼缸', roomId: 'demo-one', waterTemperature: fresh(25.6), history, light: '开启', filtration: '运行中', nextFeeding: null, filterRemainingPercent: 6 }, weather: null };
}
it('selects a configured room on focus and never changes read-only observed device state on OK', () => {
  const model = fixture();
  render(<HouseDashboard model={model} timezone="UTC" />);
  const room = screen.getByRole('button', { name: /示例二区，温度/ });
  fireEvent.focus(room);
  const side = screen.getByRole('complementary', { name: '选中房间' });
  expect(within(side).getByText('示例二区')).toBeDefined(); expect(within(side).getByText('上次观测 · 状态待更新')).toBeDefined();
  const device = within(side).getByRole('button', { name: /示例空调/ });
  fireEvent.keyDown(device, { key: 'Enter' });
  expect(within(device).getByText('待机')).toBeDefined(); expect(model.devices[1]?.state).toBe('off');
  expect(screen.getByRole('status').textContent).toContain('设备控制尚未接入');
  expect(screen.queryByRole('switch')).toBeNull();
});
it('preserves focused room during observation refresh and removes old device details when model authorization clears', () => {
  const model = fixture();
  const view = render(<HouseDashboard model={model} timezone="UTC" />);
  const room = screen.getByRole('button', { name: /示例二区，温度/ }); room.focus();
  view.rerender(<HouseDashboard model={{ ...model, devices: [] }} timezone="UTC" />);
  expect(document.activeElement).toBe(room);
  expect(screen.queryByRole('button', { name: /示例空调/ })).toBeNull();
  expect(screen.getByText('尚未接入房间设备')).toBeDefined();
});
it('renders measured chart gaps, a reference comfort line, real aquarium status and an unknown feeding time', () => {
  render(<HouseDashboard model={fixture()} timezone="UTC" />);
  const charts = screen.getAllByRole('img', { name: '近24小时实测曲线，缺测处断开' });
  expect(charts).toHaveLength(3);
  const humidity = document.querySelector('.house-chart--humidity')!;
  expect(humidity.querySelector('path[stroke-dasharray]')).not.toBeNull();
  expect(document.querySelector('.house-chart--temperature')!.querySelectorAll('path[stroke="#d5cd73"]')).toHaveLength(2);
  expect(screen.getByText('25.6')).toBeDefined(); expect(screen.getByText('滤棉需要更换')).toBeDefined();
  expect(screen.getByText('剩余 6%')).toBeDefined(); expect(screen.queryByText('明天 08:00')).toBeNull();
});
it('reports IoT loading/failure/stale honestly in the title instead of presenting a normal unconfigured device state', () => {
  const model: HouseDashboardModel = { rooms: [], devices: [], temperature: null, humidity: null, aquarium: null, weather: null };
  const view = render(<HouseDashboard model={model} timezone="UTC" deviceAvailability="loading" />);
  expect(screen.getByText('正在读取设备')).toBeDefined(); expect(screen.getByText('灯光状态待确认')).toBeDefined();
  view.rerender(<HouseDashboard model={model} timezone="UTC" deviceAvailability="failed" />);
  expect(screen.getByText('设备读取失败')).toBeDefined(); expect(screen.queryByText('设备待配置')).toBeNull();
  view.rerender(<HouseDashboard model={model} timezone="UTC" deviceAvailability="stale" unmappedDevices />);
  expect(screen.getByText('设备状态待更新')).toBeDefined();
  view.rerender(<HouseDashboard model={model} timezone="UTC" deviceAvailability="available" unmappedDevices />);
  expect(screen.getByText('展示映射待配置')).toBeDefined(); expect(screen.getByText('灯光待配置')).toBeDefined();
  expect(screen.getByText('鱼缸展示待配置')).toBeDefined(); expect(screen.queryByText('鱼缸尚未接入')).toBeNull();
});
