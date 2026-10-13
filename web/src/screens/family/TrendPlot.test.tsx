import { render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';
import { TrendPlot } from './TrendPlot';
const day = 86400000;
it('renders numeric unit ticks and positions observations by elapsed time', () => {
 const {container}=render(<TrendPlot label="身高趋势" unit="cm" points={[{time:0,value:'50.00'},{time:day,value:'51.25'},{time:10*day,value:'55.50'}]} formatTime={t=>`${t/day}日`}/>);
 expect(screen.getByRole('img',{name:'身高趋势'})).toBeTruthy();
 expect(container.querySelectorAll('[data-axis="y"] text').length).toBeGreaterThanOrEqual(3);
 expect(container.querySelector('[data-axis="y"]')?.textContent).toContain('cm');
 expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('日期');
 const xs=[...container.querySelectorAll('circle')].map(el=>Number(el.getAttribute('cx')));
 expect((xs[1]!-xs[0]!)/(xs[2]!-xs[0]!)).toBeCloseTo(.1);
 expect(container.querySelectorAll('title')[0]?.textContent).toContain('50.00 cm');
});
it('keeps a single constant observation visible without nonfinite coordinates',()=>{
 const {container}=render(<TrendPlot label="体重趋势" unit="kg" points={[{time:day,value:'6.000'}]} formatTime={()=>'2026/10/13'}/>);
 expect(container.querySelectorAll('circle')).toHaveLength(1);
 expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
 expect(container.querySelector('[data-axis="x"]')?.textContent).toContain('2026/10/13');
});
