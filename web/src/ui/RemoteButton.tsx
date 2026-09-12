import { forwardRef } from 'react';
import type { ComponentPropsWithoutRef } from 'react';
import { mapRemoteKey } from './keys';
import type { RemoteKey } from './keys';

/** Normal click semantics plus TV aliases; consumed keys never reach global Back. */
export const RemoteButton = forwardRef<HTMLButtonElement, ComponentPropsWithoutRef<'button'> & {
  onDirection?: (key: RemoteKey) => void;
}>(function RemoteButton({ onDirection, onKeyDown, ...props }, ref) {
  return <button {...props} ref={ref} type="button" onKeyDown={event => {
    onKeyDown?.(event);
    if (event.defaultPrevented) return;
    const key = mapRemoteKey(event);
    if (key === 'enter') {
      event.preventDefault();
      event.stopPropagation();
      if (!event.repeat) event.currentTarget.click();
    } else if (key && key !== 'back' && onDirection) {
      event.preventDefault();
      event.stopPropagation();
      onDirection(key);
    }
  }} />;
});
