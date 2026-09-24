import { signal } from '@preact/signals';

// The phone breakpoint (docs/06 "Mobile"), for the one component that changes
// by width rather than by CSS: the navigation shell, which is a top bar or a
// bottom tab bar (TKT-01M29CEE4K). Everything else reflows in CSS, and
// src/styles/base.css names the same value.
export const phoneQuery = '(max-width: 719.98px)';

const mq = typeof matchMedia === 'function' ? matchMedia(phoneQuery) : undefined;

export const isPhone = signal<boolean>(mq?.matches ?? false);
mq?.addEventListener?.('change', (e) => (isPhone.value = e.matches));
