/** Static household notice (FR-05). Hidden by the server when empty. */

import type { ReactElement } from 'react';

import type { NoticeWidgetPayload } from '../types/api';

export function NoticeWidget(props: { payload: NoticeWidgetPayload }): ReactElement | null {
  const text = props.payload.text.trim();
  if (!text) return null;
  return (
    <section className="widget widget--notice" aria-label="Notice">
      <p className="notice__text">{text}</p>
    </section>
  );
}
