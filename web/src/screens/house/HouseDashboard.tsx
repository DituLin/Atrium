import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { RemoteButton } from '../../ui/RemoteButton';
import { mapRemoteKey } from '../../ui/keys';
import { focusTopNav } from '../../ui/TopNav';
import { restoreReturnFocus } from '../info/readingRegion';
import { familyClockTime } from '../info/familyDate';
import { WeatherAttribution } from '../info/WeatherAttribution';
import { buildHouseChart } from './chart';
import type { HouseDashboardModel, HouseDevice, HouseDeviceKind, HouseReading, HouseRoom, HouseSeries } from './model';
import { FLOORPLAN_PLACEHOLDERS } from './model';

const ICONS: Record<HouseDeviceKind, string> = {
  light: 'M9 18h6 M10 21h4 M12 3a6 6 0 0 0-4 10.5c.8.8 1 1.5 1 2.5h6c0-1 .2-1.7 1-2.5A6 6 0 0 0 12 3z',
  ac: 'M3 5h18v8H3z M7 10h10 M8 17l-1 3 M12 16v4 M16 17l1 3',
  aquarium: 'M3 6h18v13H3z M3 9h18 M7 14c2-2.2 4.5-2.2 6.5 0c-2 2.2-4.5 2.2-6.5 0z M13.5 14l3-2v4z',
  purifier: 'M7 3h10v18H7z M10 7h4 M12 11a3 3 0 1 0 0 6a3 3 0 0 0 0-6z',
  humidifier: 'M12 3c3 4 5 6.5 5 9a5 5 0 0 1-10 0c0-2.5 2-5 5-9z',
  curtain: 'M3 4h18 M5 4v16 M19 4v16 M5 4c3 5 3 11 0 16 M19 4c-3 5-3 11 0 16',
  robot: 'M12 3a9 9 0 1 0 0 18a9 9 0 0 0 0-18z M8 9h8',
  lock: 'M6 11h12v10H6z M8 11V8a4 4 0 0 1 8 0v3',
  heater: 'M4 5h16v6H4z M8 15c0 2-1 2-1 4 M12 15c0 2-1 2-1 4 M16 15c0 2-1 2-1 4',
  other: 'M4 4h16v16H4z M8 8h8 M8 12h8 M8 16h5',
};
const COLORS: Record<HouseDeviceKind, string> = { light: '#d8cf72', ac: '#e5b884', aquarium: '#71cbe0', purifier: '#63d6b4', humidifier: '#92c5ef', curtain: '#63d6b4', robot: '#63d6b4', lock: '#63d6b4', heater: '#e5b884', other: '#a3aaa6' };
export interface HouseDashboardHandle { focusFirstRoom(): void; focusBefore(element: HTMLElement | null): void; focusAfter(element: HTMLElement | null): void }
export interface HouseDashboardProps { model: HouseDashboardModel; timezone: string; restoreFocus?: string; sourceStatus?: ReactNode; deviceObservations?: ReactNode; unmappedDevices?: boolean; deviceAvailability?: 'available' | 'loading' | 'stale' | 'failed' }
const unknown: HouseReading = { value: null, state: 'unavailable' };
function reading(reading: HouseReading, decimals: number): string {
  return reading.value !== null && Number.isFinite(reading.value) && reading.state !== 'unavailable' ? reading.value.toFixed(decimals) : '—';
}
function DeviceIcon({ kind }: { kind: HouseDeviceKind }) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={ICONS[kind]} /></svg>;
}
function isOn(device: HouseDevice) { return device.state === 'on' && device.observation === 'available'; }
function deviceState(device: HouseDevice): string {
  if (device.observation === 'stale') return `上次观测 · ${device.description}`;
  if (device.observation === 'unavailable' || device.state === 'unavailable') return '暂时无法读取';
  return device.description || (device.state === 'on' ? '开启' : device.state === 'off' ? '关闭' : '状态未知');
}
function ToggleIndicator({ device }: { device: HouseDevice }) {
  return <span className={`house-toggle${isOn(device) ? ' house-toggle--on' : ''}`} aria-hidden="true"><span /></span>;
}
function DeviceCard({ device, roomName, row, announce }: { device: HouseDevice; roomName: string; row?: boolean; announce(message: string): void }) {
  return <RemoteButton className={`house-device${row ? ' house-device--row' : ''}`} style={{ '--device-color': COLORS[device.kind] } as CSSProperties}
    data-house-focus="device" aria-label={`${device.name}，${deviceState(device)}，只读状态，控制尚未接入`} onClick={() => announce('设备控制尚未接入，当前仅显示观测状态')}>
    <span className={`house-device__icon${isOn(device) ? ' is-on' : ''}`}><DeviceIcon kind={device.kind} /></span>
    <ToggleIndicator device={device} />
    <span className="house-device__copy"><span className="house-device__name">{device.name}</span><span className="house-device__meta">{!row ? <span>{roomName}</span> : null}<span className={isOn(device) ? 'is-on' : ''}>{deviceState(device)}</span></span></span>
    {!row ? <span className="house-device__readonly">只读 · 控制未接入</span> : null}
  </RemoteButton>;
}
function RoomButton({ room, index, selected, placeholder, devices, onSelect }: { room: HouseRoom; index: number; selected: boolean; placeholder: boolean; devices: HouseDevice[]; onSelect(): void }) {
  const rect = room.rect;
  const mine = devices.filter(device => device.roomId === room.id);
  const lit = mine.filter(device => device.kind === 'light' && isOn(device));
  const left = rect.x + 4, top = rect.y + 4;
  const corners = [rect.x === 0 && rect.y === 0, rect.x + rect.width === 1120 && rect.y === 0, rect.x + rect.width === 1120 && rect.y + rect.height === 640, rect.x === 0 && rect.y + rect.height === 640];
  return <RemoteButton className={`house-room${selected ? ' is-selected' : ''}${lit.length ? ' is-lit' : ''}${placeholder ? ' house-room--placeholder' : ''}`}
    data-house-focus="room" data-house-room={room.id} aria-pressed={selected} aria-label={placeholder ? `未配置分区 ${index + 1}` : `${room.name}，温度 ${reading(room.temperature, 1)} 度，湿度 ${reading(room.humidity, 0)}%${room.temperature.state === 'stale' || room.humidity.state === 'stale' ? '，上次观测' : ''}`}
    style={{ left: `${left / 11.2}%`, top: `${top / 6.4}%`, width: `${(rect.width - 8) / 11.2}%`, height: `${(rect.height - 8) / 6.4}%`, borderRadius: corners.map(corner => `calc(${corner ? 26 : 12} * var(--u))`).join(' ') }}
    onFocus={onSelect} onClick={onSelect}>
    {mine.filter(device => isOn(device) && (device.kind === 'light' || device.kind === 'aquarium') && device.position).map(device => <span key={device.id} aria-hidden="true" className={`house-room__glow${device.kind === 'aquarium' ? ' house-room__glow--water' : ''}`} style={{ left: `${(device.position!.x - left) / (rect.width - 8) * 100}%`, top: `${(device.position!.y - top) / (rect.height - 8) * 100}%`, opacity: device.kind === 'aquarium' ? .7 : .5 + (device.brightnessPercent ?? 50) / 200 }} />)}
    <span className="house-room__title"><span>{room.name}</span><small>{placeholder ? '待配置' : lit.length ? `${lit.length} 盏灯亮着` : mine.length ? '暂无开灯观测' : '尚未接入设备'}</small></span>
    <span className="house-room__readings"><strong>{reading(room.temperature, 1)}°</strong><span>{reading(room.humidity, 0)}%</span></span>
    {mine.filter(device => device.position).map(device => <span className={`house-room__marker${isOn(device) ? ' is-on' : ''}`} key={device.id} aria-hidden="true" style={{ left: `${(device.position!.x - left) / (rect.width - 8) * 100}%`, top: `${(device.position!.y - top) / (rect.height - 8) * 100}%`, '--device-color': COLORS[device.kind] } as CSSProperties}><DeviceIcon kind={device.kind} /></span>)}
  </RemoteButton>;
}
function ChartArtwork({ series, color, id, compact = false }: { series: HouseSeries | null; color: string; id: string; compact?: boolean }) {
  const width = compact ? 500 : 520, height = compact ? 56 : 190;
  const measured = buildHouseChart(series, width, height);
  const chart = id === 'house-humidity' ? buildHouseChart(series, width, height, [Math.min(30, measured.min ?? 30), Math.max(70, measured.max ?? 70)]) : measured;
  const bandTop = 6 + (1 - (60 - chart.low) / (chart.high - chart.low)) * (height - 12);
  const bandBottom = 6 + (1 - (40 - chart.low) / (chart.high - chart.low)) * (height - 12);
  return <div className={`house-chart__art${compact ? ' house-chart__art--compact' : ''}`}>
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" aria-label={series && chart.paths.length ? '近24小时实测曲线，缺测处断开' : '暂无24小时观测'} role="img">
      <defs><linearGradient id={id} x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor={color} stopOpacity=".42" /><stop offset="1" stopColor={color} stopOpacity="0" /></linearGradient><clipPath id={`${id}-clip`}><rect width={width} height={height} /></clipPath></defs>
      <g clipPath={`url(#${id}-clip)`}>
        {!compact && [0, 1, 2, 3].map(index => <path key={index} d={`M0,${6 + index * (height - 12) / 3}H${width}`} stroke="rgba(255,255,255,.055)" />)}
        {id === 'house-humidity' && series ? <rect x="0" y={bandTop} width={width} height={Math.max(0, bandBottom - bandTop)} fill="rgba(255,255,255,.05)" /> : null}
        {chart.paths.map((path, index) => <path key={`area-${index}`} d={path.area} fill={`url(#${id})`} />)}
        {id === 'house-humidity' && series && !series.reference ? <path d={`M0,${bandTop}H${width}`} fill="none" stroke="#7d8480" strokeWidth="2.5" strokeDasharray="7 7" /> : null}
        {chart.reference.map((path, index) => <path key={`ref-${index}`} d={path.line} fill="none" stroke="#7d8480" strokeWidth="2.5" strokeDasharray="7 7" strokeLinecap="round" />)}
        {chart.paths.map((path, index) => <path key={`line-${index}`} d={path.line} fill="none" stroke={color} strokeWidth={compact ? 3.5 : 4} strokeLinecap="round" strokeLinejoin="round" />)}
        {chart.last ? <circle cx={chart.last.x} cy={chart.last.y} r={compact ? 4 : 7} fill={color} stroke="#171a19" strokeWidth="3" /> : null}
      </g>
    </svg>
    {!chart.paths.length ? <span className="house-chart__empty">{compact ? '尚无水温记录' : '暂无 24 小时观测'}</span> : null}
  </div>;
}
function HistoryCard({ kind, series, timezone }: { kind: 'temperature' | 'humidity'; series: HouseSeries | null; timezone: string }) {
  const temperature = kind === 'temperature', color = temperature ? '#d5cd73' : '#92c5ef', unit = temperature ? '°C' : '%';
  const chart = buildHouseChart(series), digits = temperature ? 1 : 0;
  const end = Date.parse(series?.endAt ?? '');
  const labels = Array.from({ length: 5 }, (_, index) => !Number.isFinite(end) ? ['24 小时前', '18 小时前', '12 小时前', '6 小时前', '现在'][index] : index === 4 ? '现在' : familyClockTime(new Date(end - (4 - index) * 6 * 3600000).toISOString(), timezone, false));
  return <RemoteButton className={`house-chart house-chart--${kind}`} data-house-focus="chart" aria-label={`${temperature ? '温度' : '湿度'}，${series ? '近24小时观测' : '尚未接入'}`}>
    <span className="house-chart__heading"><span style={{ color }}>{temperature ? '温度' : '湿度'}</span><strong>{reading(series?.current ?? unknown, digits)}</strong><span className="house-chart__unit">{unit}</span></span>
    <span className="house-chart__legend"><span><i style={{ background: color }} />全屋平均</span><span><i className="house-chart__reference" />{series?.reference?.label ?? (temperature ? '室外未接入' : '舒适上限 60%')}</span><span>{chart.min !== null ? `${chart.min.toFixed(digits)} – ${chart.max!.toFixed(digits)}${temperature ? '°' : '%'}` : '暂无记录'}</span></span>
    <ChartArtwork series={series} color={color} id={`house-${kind}`} />
    <span className="house-chart__axis">{labels.map((label, index) => <span key={index}>{label}</span>)}</span>
    {series?.current.state === 'stale' ? <span className="house-observation-note">上次观测 · 状态待更新</span> : null}
  </RemoteButton>;
}
function AquariumCard({ model, mappingPending }: { model: HouseDashboardModel; mappingPending: boolean }) {
  const tank = model.aquarium, room = model.rooms.find(room => room.id === tank?.roomId);
  const remaining = tank?.filterRemainingPercent;
  const percent = typeof remaining === 'number' && Number.isFinite(remaining) && remaining >= 0 && remaining <= 100 ? remaining : null;
  return <RemoteButton className="house-aquarium" data-house-focus="aquarium" aria-label={tank ? `${tank.name}，鱼缸只读状态` : mappingPending ? '鱼缸展示待配置' : '鱼缸尚未接入'}>
    <span className="house-aquarium__title"><i />{tank ? `${tank.name}${room ? ` · ${room.name}` : ''}` : '智能鱼缸'}</span>
    <span className="house-aquarium__temperature"><span>水温</span><strong>{reading(tank?.waterTemperature ?? unknown, 1)}</strong><span>°C</span><small>{tank ? '只读观测' : '尚未接入'}</small></span>
    <ChartArtwork series={tank?.history ?? null} color="#71cbe0" id="house-aquarium" compact />
    <span className="house-aquarium__stats">{[{ label: '灯光', value: tank?.light }, { label: '过滤', value: tank?.filtration }, { label: '下次喂食', value: tank?.nextFeeding }].map(stat => <span key={stat.label}><small>{stat.label}</small><strong>{stat.value ?? '—'}</strong></span>)}</span>
    <span className="house-aquarium__filter"><span><strong>{percent === null ? mappingPending ? '鱼缸展示待配置' : '鱼缸尚未接入' : percent <= 10 ? '滤棉需要更换' : '滤棉余量'}</strong><span>{percent === null ? mappingPending ? '等待展示映射' : '等待设备数据' : `剩余 ${percent}%`}</span></span><i><span style={{ width: `${percent ?? 0}%` }} /></i></span>
    {tank?.waterTemperature.state === 'stale' ? <span className="house-observation-note">上次观测 · 状态待更新</span> : null}
  </RemoteButton>;
}
function EmptyDevices({ title, mappingPending }: { title: string; mappingPending: boolean }) {
  return <><RemoteButton className="house-device house-device--empty" data-house-focus="empty" aria-label={mappingPending ? `${title}分类尚未配置` : `${title}设备尚未接入`}><span className="house-device__icon"><DeviceIcon kind="other" /></span><span className="house-device__name">{mappingPending ? `尚未配置${title}分类` : `尚未接入${title}设备`}</span><span className="house-device__meta">{mappingPending ? '已接入设备显示于上方观测区' : '等待设备配置与观测'}</span></RemoteButton>{[1, 2, 3].map(index => <span key={index} className="house-device house-device--ghost" aria-hidden="true"><span /><span /><span /></span>)}</>;
}

export const HouseDashboard = forwardRef<HouseDashboardHandle, HouseDashboardProps>(function HouseDashboard({ model, timezone, restoreFocus, sourceStatus, deviceObservations, unmappedDevices = false, deviceAvailability }, ref) {
  const scroll = useRef<HTMLElement>(null);
  const initialRoom = model.rooms[3]?.id ?? model.rooms[0]?.id ?? 'placeholder-3';
  const firstRoomId = useRef(initialRoom);
  const [selectedId, setSelectedId] = useState(initialRoom);
  const [announcement, setAnnouncement] = useState('');
  const rooms: HouseRoom[] = model.rooms.length ? model.rooms : FLOORPLAN_PLACEHOLDERS.map((rect, index) => ({ id: `placeholder-${index}`, name: `分区 ${String(index + 1).padStart(2, '0')}`, rect: { ...rect }, temperature: unknown, humidity: unknown }));
  const selected = rooms.find(room => room.id === selectedId) ?? rooms.find(room => room.id === initialRoom)!;
  const selectedDevices = model.devices.filter(device => device.roomId === selected.id);
  const first = () => Array.from(scroll.current?.querySelectorAll<HTMLElement>('[data-house-room]') ?? []).find(element => element.dataset.houseRoom === firstRoomId.current) ?? scroll.current?.querySelector<HTMLElement>('[data-house-room]') ?? null;
  useImperativeHandle(ref, () => ({ focusFirstRoom() { first()?.focus(); }, focusBefore(element) { const nodes = Array.from(scroll.current?.querySelectorAll<HTMLElement>('[data-house-focus]') ?? []).filter(node => element?.dataset.houseFocus !== 'sources' || node.dataset.houseFocus !== 'refresh'); const index = element ? nodes.indexOf(element) : -1; (nodes[Math.max(0, index - 1)] ?? first())?.focus(); }, focusAfter(element) { const nodes = Array.from(scroll.current?.querySelectorAll<HTMLElement>('[data-house-focus]') ?? []); const index = element ? nodes.indexOf(element) : -1; nodes[index + 1]?.focus(); } }));
  useEffect(() => { restoreReturnFocus(restoreFocus, Array.from(scroll.current?.querySelectorAll<HTMLElement>('[data-house-room]') ?? []).find(element => element.dataset.houseRoom === firstRoomId.current) ?? null); }, [restoreFocus]); // Observations never steal focus.
  const waitingForDevices = deviceAvailability !== undefined && deviceAvailability !== 'available';
  const moduleStatus = deviceAvailability === 'loading' ? '正在读取设备' : deviceAvailability === 'failed' ? '设备读取失败' : deviceAvailability === 'stale' ? '设备状态待更新' : null;
  const lights = model.devices.filter(device => device.kind === 'light'), acs = model.devices.filter(device => device.kind === 'ac');
  const lightsOn = lights.filter(isOn).length, acsOn = acs.filter(isOn).length;
  const weatherObserved = familyClockTime(model.weather?.observedAt ?? null, timezone, false);
  const weatherUpdated = familyClockTime(model.weather?.updatedAt ?? null, timezone, false);
  const groups = [
    { title: '灯光', kinds: ['light'], sub: lights.length ? `${lightsOn} / ${lights.length} 盏亮着` : unmappedDevices ? '设备分类待配置' : '设备尚未接入' },
    { title: '空调与空气', kinds: ['ac', 'purifier', 'humidifier', 'heater'], sub: unmappedDevices ? '设备分类待配置' : '只读设备观测' },
    { title: '门窗与清洁', kinds: ['lock', 'curtain', 'robot', 'other'], sub: unmappedDevices ? '设备分类待配置' : '只读设备观测' },
  ];
  const keepVisible = (element: HTMLElement) => {
    const container = scroll.current;
    if (!container || !element.matches('[data-house-focus]')) return;
    if (element.dataset.houseFocus === 'room' || element.classList.contains('house-device--row')) { container.scrollTop = 0; return; }
    const cr = container.getBoundingClientRect(), er = element.getBoundingClientRect();
    if (er.top < cr.top + 24) container.scrollTop += er.top - cr.top - 24;
    else if (er.bottom > cr.bottom - 32) container.scrollTop += er.bottom - cr.bottom + 32;
  };
  return <main className="house-dashboard" ref={scroll} aria-label="房屋仪表盘" onFocusCapture={event => keepVisible(event.target as HTMLElement)} onKeyDown={event => {
    const key = mapRemoteKey(event);
    if (!key || key === 'back' || key === 'enter' || event.defaultPrevented) return;
    const target = event.target as HTMLElement;
    const nodes = Array.from(scroll.current?.querySelectorAll<HTMLElement>('[data-house-focus]') ?? []);
    if (!nodes.includes(target)) return;
    event.preventDefault(); event.stopPropagation();
    const rect = target.getBoundingClientRect(), cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2;
    const horizontal = key === 'left' || key === 'right';
    const candidates = nodes.filter(node => node !== target).map(node => {
      const r = node.getBoundingClientRect();
      const gap = key === 'right' ? r.left - rect.right : key === 'left' ? rect.left - r.right : key === 'down' ? r.top - rect.bottom : rect.top - r.bottom;
      const orthogonal = horizontal ? Math.abs(r.top + r.height / 2 - cy) : Math.abs(r.left + r.width / 2 - cx);
      const overlap = horizontal ? r.top < rect.bottom && r.bottom > rect.top : r.left < rect.right && r.right > rect.left;
      return { node, r, gap, score: Math.max(gap, 0) + orthogonal * 1.6, overlap };
    }).filter(candidate => candidate.gap >= -8 && candidate.r.width > 0);
    const overlapping = candidates.filter(candidate => candidate.overlap);
    const next = (overlapping.length ? overlapping : candidates).sort((a, b) => a.score - b.score)[0]?.node;
    if (next) next.focus();
    else if (!rect.width) { const index = nodes.indexOf(target); const direction = key === 'right' || key === 'down' ? 1 : -1; if (nodes[index + direction]) nodes[index + direction]!.focus(); else if (key === 'up') focusTopNav(); }
    else if (key === 'up') focusTopNav();
  }}>
    <div className="house-dashboard__heading"><h1>房屋</h1><div className="house-dashboard__chips">
      <span className="house-chip house-chip--light"><i />{lights.length ? `${lightsOn} 盏灯亮着` : waitingForDevices ? '灯光状态待确认' : unmappedDevices ? '灯光待配置' : '灯光未接入'}</span>
      <span className="house-chip house-chip--climate"><i />{acs.length ? `${acsOn} 台空调运行` : waitingForDevices ? '空调状态待确认' : unmappedDevices ? '空调待配置' : '空调未接入'}</span>
      <span className="house-chip"><i />{model.temperature || model.humidity ? `室内 ${reading(model.temperature?.current ?? unknown, 1)}° · ${reading(model.humidity?.current ?? unknown, 0)}%` : waitingForDevices ? '室内环境待确认' : unmappedDevices ? '室内环境待配置' : '室内环境未接入'}</span>
      <span className="house-chip house-chip--attention"><i />{moduleStatus ?? (model.devices.length ? `${model.devices.filter(device => device.observation !== 'available').length} 项状态待确认` : unmappedDevices ? '展示映射待配置' : '设备待配置')}</span>
    </div><div className="house-dashboard__weather"><span>{model.weather?.temperature !== null && model.weather?.temperature !== undefined ? `室外 ${model.weather.temperature.toFixed(0)}°${model.weather.text ? ` · ${model.weather.text}` : ''}${model.weather.stale ? ' · 上次观测' : ''}${weatherObserved ? ` · 观测于 ${weatherObserved}` : ''}${weatherUpdated ? ` · ${weatherObserved ? '刷新于' : '更新于'} ${weatherUpdated}` : ''}` : '室外天气未接入'}</span>
      <WeatherAttribution provider={model.weather?.provider} attributions={model.weather?.attributions} houseFocus onTop={() => focusTopNav()} onLeft={() => first()?.focus()} onBottom={() => first()?.focus()} />
    </div></div>
    <section className="house-overview">
      <div className="house-floorplan" role="region" aria-label="房屋平面图">{rooms.map((room, index) => <RoomButton key={room.id} room={room} index={index} selected={room.id === selected.id} placeholder={!model.rooms.length} devices={model.devices} onSelect={() => setSelectedId(room.id)} />)}{!model.rooms.length ? <span className="house-floorplan__notice">户型示意 · 房间尚未配置</span> : null}</div>
      <aside className="house-selected" aria-label="选中房间"><div className="house-selected__heading"><strong>{selected.name}</strong><span>{model.rooms.length ? `${selectedDevices.length} 台设备${selectedDevices.some(device => device.kind === 'light' && isOn(device)) ? ` · ${selectedDevices.filter(device => device.kind === 'light' && isOn(device)).length} 盏灯亮着` : ''}` : '待配置分区'}</span></div>
        <div className="house-selected__readings"><span><small>温度</small><strong>{reading(selected.temperature, 1)}°</strong></span><span><small>湿度</small><strong>{reading(selected.humidity, 0)}%</strong></span></div>
        {selected.temperature.state === 'stale' || selected.humidity.state === 'stale' ? <span className="house-observation-note">上次观测 · 状态待更新</span> : null}
        <div className="house-selected__devices">{selectedDevices.length ? selectedDevices.map(device => <DeviceCard key={device.id} device={device} row roomName={selected.name} announce={setAnnouncement} />) : <div className="house-selected__empty"><span className="house-selected__empty-icon"><DeviceIcon kind="other" /></span><strong>{model.rooms.length ? '尚未接入房间设备' : unmappedDevices ? '户型关联尚未配置' : '房间与设备尚未配置'}</strong><p>{unmappedDevices ? '已接入设备和读数显示于下方观测区' : '接入后显示实时观测与设备状态'}</p><span>温度 —°<br />湿度 —%</span></div>}</div>
        <span className="house-selected__hint">← → 选择房间 · 设备状态只读</span>
      </aside>
    </section>
    <h2 className="house-section-title">温湿度 · 近 24 小时</h2>
    <section className="house-history" aria-label="环境与鱼缸"><HistoryCard kind="temperature" series={model.temperature} timezone={timezone} /><HistoryCard kind="humidity" series={model.humidity} timezone={timezone} /><AquariumCard model={model} mappingPending={unmappedDevices} /></section>
    {deviceObservations}
    {groups.map(group => { const devices = model.devices.filter(device => group.kinds.includes(device.kind)); return <section key={group.title} className="house-group" aria-label={group.title}><div className="house-group__heading"><h2>{group.title}</h2><span>{group.sub}</span></div><div className="house-group__devices">{devices.length ? devices.map(device => <DeviceCard key={device.id} device={device} roomName={model.rooms.find(room => room.id === device.roomId)?.name ?? '未分配房间'} announce={setAnnouncement} />) : <EmptyDevices title={group.title} mappingPending={unmappedDevices} />}</div></section>; })}
    {sourceStatus}
    <span className="house-dashboard__announcement" role="status" aria-live="polite">{announcement}</span>
  </main>;
});
