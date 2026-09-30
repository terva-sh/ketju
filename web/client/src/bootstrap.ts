import type { Me } from './api/dto';

export type { Me };

// The viewer the server rendered into the page (docs/07 "Embedding"): the
// shell carries /me as a JSON data block so the first paint needs no round
// trip. It is never executed, which is why the CSP allows it.

export const anonymous: Me = {
  user: null,
  pre_user: null,
  unread: { threads: 0, participated: 0, watching: 0 },
  notifications: 0,
  queue: null,
  settings: { timezone: '', locale: '', page_size: 25 },
  site_name: 'Ketju',
};

// readBootstrap parses the data block, and falls back to an anonymous viewer
// when it does not parse. Under the Vite dev server the block is the raw
// template placeholder, because Vite, not Go, served the page.
export function readBootstrap(doc: Document = document): Me {
  const text = doc.getElementById('bootstrap')?.textContent ?? '';
  try {
    const me = JSON.parse(text) as Me;
    return me && typeof me === 'object' ? me : anonymous;
  } catch {
    return anonymous;
  }
}
