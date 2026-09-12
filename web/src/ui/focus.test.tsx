import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Focusable, FocusGroup } from './focus';
import { useRemoteKeys } from '../app/useTick';

function GlobalKeys({ onKey }: { onKey: () => void }) {
  useRemoteKeys(onKey);
  return null;
}

describe('remote event ownership', () => {
  it.each(['Enter', 'OK', 'Select', ' ', 'Spacebar'])('activates exactly once for %s', (key) => {
    const activate = vi.fn();
    const global = vi.fn();
    render(<><GlobalKeys onKey={global} /><FocusGroup count={1} onActivate={activate}>
      <Focusable index={0} onActivate={activate}>照片</Focusable>
    </FocusGroup></>);
    fireEvent.keyDown(screen.getByRole('button'), { key });
    expect(activate).toHaveBeenCalledTimes(1);
    expect(global).not.toHaveBeenCalled();
  });

  it('does not steal focus when an inactive controlled grid changes', () => {
    const view = (index: number) => <><button>合集</button><FocusGroup count={2} index={index} active={false}>
      <Focusable index={0}>一</Focusable><Focusable index={1}>二</Focusable>
    </FocusGroup></>;
    const { rerender } = render(view(0));
    screen.getByText('合集').focus();
    rerender(view(1));
    expect(document.activeElement).toBe(screen.getByText('合集'));
  });

  it('leaves Back for its owner when another screen listener ignores it', () => {
    const owner = vi.fn(() => true);
    render(<><GlobalKeys onKey={() => {}} /><GlobalKeys onKey={owner} /></>);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(owner).toHaveBeenCalledTimes(1);
  });

  it('does not dispatch global Back when locally consumed', () => {
    const global = vi.fn();
    render(<><GlobalKeys onKey={global} /><button onKeyDown={event => event.preventDefault()}>本地返回</button></>);
    fireEvent.keyDown(screen.getByRole('button'), { key: 'Escape' });
    expect(global).not.toHaveBeenCalled();
  });
});
