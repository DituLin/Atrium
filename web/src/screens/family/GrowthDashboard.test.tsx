import {render,screen,within} from '@testing-library/react';
import {expect,it} from 'vitest';
import type {ArchiveAnalysis} from '../../types/archives';
import {GrowthDashboard} from './GrowthDashboard';
import {ArchiveAnalysisPanel} from './ArchiveAnalysisPanel';
const analysis=(groups:ArchiveAnalysis['groups']):ArchiveAnalysis=>({snapshot_id:'test',data_epoch:1,generation:1,projection:'public',algorithm_version:'test',groups,coverage:{complete:true,included:groups.length,excluded:0,reasons:[]}});
const mixed=[{metric:'height',date:'2026-10-13',value:'70.00',unit:'cm'},{metric:'height',date:'2026-10-12T23:30:00-07:00',value:'69.00',unit:'cm'}];
it('uses the same household dates for latest measurement, table order and plot',()=>{
 render(<GrowthDashboard name="合成宝宝" data={{height:[analysis(mixed)],weight:[],limited:[]}} period="all" timezone="America/Los_Angeles" busy={false} onPeriod={()=>{}} onLeft={()=>{}} onUp={()=>{}} onDown={()=>{}} onRead={()=>{}}/>);
 expect(screen.getByRole('img',{name:/身高趋势/}).getAttribute('aria-label')).toContain('最新 70.00 cm');
 const rows=within(screen.getByRole('table')).getAllByRole('row');expect(rows[1]?.textContent).toContain('2026/10/13');
});
it('does not shift a literal date backward when mixed with zoned archive observations',()=>{
 const {container}=render(<ArchiveAnalysisPanel analysis={analysis(mixed)} dateLabel={s=>s} timezone="America/Los_Angeles"/>);
 const titles=[...container.querySelectorAll('circle title')].map(el=>el.textContent);
 expect(titles).toContain('2026/10/13 · 70.00 cm');expect(titles).toContain('2026/10/12 · 69.00 cm');
});
it('keeps different units separate and shows explicit empty weight state',()=>{
 render(<GrowthDashboard name="合成宝宝" data={{height:[analysis([...mixed,{metric:'height',date:'2026-10-14',value:'0.71',unit:'m'}])],weight:[],limited:[]}} period="all" timezone="UTC" busy={false} onPeriod={()=>{}} onLeft={()=>{}} onUp={()=>{}} onDown={()=>{}} onRead={()=>{}}/>);
 expect(screen.getAllByRole('img',{name:/身高趋势/})).toHaveLength(2);
 expect(screen.getByText('暂无日期、数值和单位完整的体重记录')).toBeTruthy();
});
