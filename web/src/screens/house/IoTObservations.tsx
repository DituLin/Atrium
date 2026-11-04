import { useRef } from 'react';
import type { HouseIoT, IoTCapability, IoTDevice } from '../../types/api';
import { familyTime } from '../info/familyDate';
import { onReadingKey } from '../info/readingRegion';
const LABELS: Record<IoTCapability['kind'], string> = { power: '电源', humidity: '湿度', temperature: '温度', operating_state: '运行状态', battery: '电量', filter_life: '滤芯寿命' };
function valueOf(capability: IoTCapability): string {
  const { value, availability, unit } = capability.observation;
  if (availability === 'unavailable') return '不可用';
  if (availability === 'unknown' || value === null) return '未知';
  if (typeof value === 'boolean') return value ? '开启' : '关闭';
  if (typeof value === 'number') return Number.isFinite(value) ? `${value}${unit ? ` ${unit}` : ''}` : '未知';
  return { off: '关闭', idle: '待机', active: '运行中', paused: '已暂停', error: '异常' }[value] ?? '未知';
}
export interface IoTObservationsProps {
  iot: HouseIoT; stale: boolean; timezone: string;
  onLeave?(element: HTMLElement, direction: 'up' | 'down' | 'left' | 'right'): void;
}
/** Direct read-only projection. Names never determine device types, aquarium roles,
 * room geometry, or which humidity is an ambient/target measurement. */
export function IoTObservations({ iot, stale, timezone, onLeave }: IoTObservationsProps) {
  const region = useRef<HTMLElement>(null);
  const availability = stale && iot.availability === 'available' ? 'stale' : iot.availability;
  const groups = new Map<string, { name: string; devices: Array<{ device: IoTDevice; instance: HouseIoT['instances'][number] }> }>();
  for (const instance of iot.instances) for (const device of instance.devices) {
    const key = device.room_id || '';
    if (!groups.has(key)) groups.set(key, { name: device.room_name || '未分配房间', devices: [] });
    groups.get(key)!.devices.push({ device, instance });
  }
  const leave = (direction: 'up' | 'down' | 'left' | 'right') => { if (region.current) onLeave?.(region.current, direction); };
  return <section className="house-live" aria-label="已接入设备" role="region" ref={region} tabIndex={0} data-house-focus="observations" onKeyDown={event => onReadingKey(event, { top: () => leave('up'), bottom: () => leave('down'), left: () => leave('left'), right: () => leave('right') })}>
    <div className="house-group__heading"><h2>已接入设备</h2><span>按房间分组 · 只读观测</span></div>
    <p className="house-live__mapping-note">户型位置与设备分类尚未配置，以下显示已授权的真实设备数据。</p>
    {availability !== 'available' ? <p className="house-live__status">{availability === 'stale' ? '设备状态待更新 · 显示上次观测' : availability === 'loading' ? '设备状态等待读取' : '暂时无法读取设备状态'}</p> : null}
    <div className="house-live__sources">{iot.instances.map(instance => <div key={instance.id} className="house-live__source"><span><i className={instance.status === 'connected' && availability === 'available' ? 'is-connected' : ''} />Home Assistant · {instance.status === 'connected' ? '已连接' : instance.status === 'connecting' ? '正在连接' : '未连接'}</span><span>最近同步 <time dateTime={instance.last_success_at ?? undefined}>{familyTime(instance.last_success_at, timezone, true)}</time></span></div>)}</div>
    {[...groups].map(([key, group]) => <section key={key} className="house-live__room" aria-label={group.name}><h3>{group.name}</h3><div className="house-live__devices">{group.devices.map(({ device, instance }) => {
      const old = stale || availability !== 'available' || instance.status !== 'connected';
      return <article key={`${instance.id}/${device.id}`} className="house-live__device" aria-label={device.name}>
        <header><h4>{device.name}</h4>{old ? <span className="house-live__old">上次观测</span> : null}</header>
        <dl className="house-live__capabilities">{device.capabilities.map(capability => {
          const observation = capability.observation;
          return <div key={capability.id} className="house-live__capability"><dt>{capability.name || `${LABELS[capability.kind]} · ${capability.id}`}</dt><dd><strong className={observation.availability === 'available' && !old ? 'is-current' : ''}>{valueOf(capability)}</strong><dl className="house-live__times">{[
            { label: '来源变更', value: observation.source_updated_at }, { label: '测量时间', value: observation.sampled_at },
            { label: '接收时间', value: observation.received_at }, { label: '同步确认', value: observation.confirmed_at },
          ].map(time => <div key={time.label}><dt>{time.label}</dt><dd>{time.value ? <time dateTime={time.value}>{familyTime(time.value, timezone, true)}</time> : '未提供'}</dd></div>)}</dl></dd></div>;
        })}</dl>
      </article>;
    })}</div></section>)}
    {!groups.size && availability === 'available' ? <p className="house-live__status">没有可展示的设备</p> : null}
  </section>;
}
